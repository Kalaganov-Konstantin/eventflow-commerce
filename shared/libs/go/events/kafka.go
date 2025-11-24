package events

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

const (
	defaultMaxRetries     = 3
	defaultRetryBaseDelay = 100 * time.Millisecond
)

type KafkaConfig struct {
	Brokers        []string      `mapstructure:"KAFKA_BROKERS"`
	GroupID        string        `mapstructure:"KAFKA_GROUP_ID"`
	DLQTopic       string        `mapstructure:"KAFKA_DLQ_TOPIC"`
	MaxRetries     int           `mapstructure:"KAFKA_MAX_RETRIES"`
	RetryBaseDelay time.Duration `mapstructure:"KAFKA_RETRY_BASE_DELAY"`
}

type Event struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`
	Source        string                 `json:"source"`
	Data          map[string]interface{} `json:"data"`
	Timestamp     time.Time              `json:"timestamp"`
	Version       string                 `json:"version"`
	CorrelationID string                 `json:"correlationId,omitempty"`
}

// kafkaWriter is the subset of *kafka.Writer used for publishing, extracted so tests can
// substitute a fake writer.
type kafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// kafkaReader is the subset of *kafka.Reader used by Subscriber, extracted so tests can
// substitute a fake reader.
type kafkaReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type Publisher struct {
	writer kafkaWriter
}

type Subscriber struct {
	reader         kafkaReader
	logger         *zap.Logger
	dlqWriter      kafkaWriter
	maxRetries     int
	retryBaseDelay time.Duration
}

func NewPublisher(config KafkaConfig) *Publisher {
	writer := &kafka.Writer{
		Addr:         kafka.TCP(config.Brokers...),
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		Compression:  kafka.Snappy,
	}

	return &Publisher{writer: writer}
}

func NewSubscriber(config KafkaConfig, topic string, logger *zap.Logger) *Subscriber {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     config.Brokers,
		Topic:       topic,
		GroupID:     config.GroupID,
		MinBytes:    10e3, // 10KB
		MaxBytes:    10e6, // 10MB
		MaxWait:     1 * time.Second,
		StartOffset: kafka.LastOffset,
	})

	var dlqWriter *kafka.Writer
	if config.DLQTopic != "" {
		dlqWriter = &kafka.Writer{
			Addr:     kafka.TCP(config.Brokers...),
			Balancer: &kafka.LeastBytes{},
			Topic:    config.DLQTopic,
		}
	}

	maxRetries := config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	retryBaseDelay := config.RetryBaseDelay
	if retryBaseDelay <= 0 {
		retryBaseDelay = defaultRetryBaseDelay
	}

	return &Subscriber{
		reader:         reader,
		logger:         logger,
		dlqWriter:      dlqWriter,
		maxRetries:     maxRetries,
		retryBaseDelay: retryBaseDelay,
	}
}

func (p *Publisher) Publish(ctx context.Context, topic string, event Event) error {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.Version == "" {
		event.Version = "1.0"
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	message := kafka.Message{
		Topic: topic,
		Key:   []byte(event.ID),
		Value: data,
		Headers: []kafka.Header{
			{Key: "eventType", Value: []byte(event.Type)},
			{Key: "source", Value: []byte(event.Source)},
			{Key: "version", Value: []byte(event.Version)},
		},
	}

	if event.CorrelationID != "" {
		message.Headers = append(message.Headers, kafka.Header{
			Key:   "correlationId",
			Value: []byte(event.CorrelationID),
		})
	}

	return p.writer.WriteMessages(ctx, message)
}

// Subscribe fetches messages one at a time and only commits an offset after the message was
// either handled successfully or safely handed off to the DLQ, so a handler failure combined
// with an unavailable DLQ leaves the offset uncommitted for redelivery.
func (s *Subscriber) Subscribe(ctx context.Context, handler func(Event) error) error {
	for {
		msg, err := s.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.logger.Error("Failed to fetch message from Kafka", zap.Error(err))
			continue
		}

		s.processMessage(ctx, msg, handler)
	}
}

// processMessage unmarshals and handles a single fetched message, routing failures to the DLQ
// and committing the offset only once the message has been safely dealt with.
func (s *Subscriber) processMessage(ctx context.Context, msg kafka.Message, handler func(Event) error) {
	var event Event
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		s.logger.Error("Failed to unmarshal Kafka message", zap.Error(err), zap.ByteString("message", msg.Value))
		s.handleFailure(ctx, msg, "unmarshal_error")
		return
	}

	if err := s.handleWithRetry(ctx, event, handler); err != nil {
		s.logger.Error("Failed to handle event after retries", zap.Error(err), zap.String("event_id", event.ID))
		s.handleFailure(ctx, msg, "handler_error")
		return
	}

	s.commit(ctx, msg)
}

// handleWithRetry calls handler, retrying up to maxRetries times with exponential backoff and
// jitter between attempts before giving up.
func (s *Subscriber) handleWithRetry(ctx context.Context, event Event, handler func(Event) error) error {
	err := handler(event)
	for attempt := 1; err != nil && attempt <= s.maxRetries; attempt++ {
		if waitErr := s.wait(ctx, s.retryDelay(attempt)); waitErr != nil {
			return waitErr
		}
		err = handler(event)
	}
	return err
}

// retryDelay returns the exponential backoff delay for the given retry attempt (1-based),
// plus up to 50% jitter.
func (s *Subscriber) retryDelay(attempt int) time.Duration {
	delay := s.retryBaseDelay << (attempt - 1)
	jitter := time.Duration(rand.Int64N(int64(delay)/2 + 1))
	return delay + jitter
}

func (s *Subscriber) wait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// handleFailure sends msg to the DLQ and only commits its offset once that write succeeds.
func (s *Subscriber) handleFailure(ctx context.Context, msg kafka.Message, errorType string) {
	if s.sendToDLQ(ctx, msg, errorType) {
		s.commit(ctx, msg)
	}
}

func (s *Subscriber) commit(ctx context.Context, msg kafka.Message) {
	if err := s.reader.CommitMessages(ctx, msg); err != nil {
		s.logger.Error("Failed to commit Kafka message offset", zap.Error(err), zap.ByteString("key", msg.Key))
	}
}

// sendToDLQ writes msg to the configured DLQ topic and reports whether it succeeded.
func (s *Subscriber) sendToDLQ(ctx context.Context, msg kafka.Message, errorType string) bool {
	if s.dlqWriter == nil {
		s.logger.Warn("DLQ topic not configured. Message will be dropped.", zap.ByteString("key", msg.Key))
		return false
	}

	msg.Headers = append(msg.Headers, kafka.Header{Key: "errorType", Value: []byte(errorType)})

	if err := s.dlqWriter.WriteMessages(ctx, msg); err != nil {
		s.logger.Error("Failed to send message to DLQ", zap.Error(err), zap.ByteString("key", msg.Key))
		return false
	}
	return true
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}

func (s *Subscriber) Close() error {
	if s.dlqWriter != nil {
		_ = s.dlqWriter.Close()
	}
	return s.reader.Close()
}

func LoadKafkaConfig() (KafkaConfig, error) {
	v := viper.New()
	v.AutomaticEnv()

	v.SetDefault("KAFKA_BROKERS", "localhost:9092")
	v.SetDefault("KAFKA_GROUP_ID", "eventflow-service")
	v.SetDefault("KAFKA_DLQ_TOPIC", "eventflow-dlq")
	v.SetDefault("KAFKA_MAX_RETRIES", defaultMaxRetries)
	v.SetDefault("KAFKA_RETRY_BASE_DELAY", defaultRetryBaseDelay)

	if err := v.BindEnv("KAFKA_BROKERS"); err != nil {
		return KafkaConfig{}, fmt.Errorf("failed to bind KAFKA_BROKERS: %w", err)
	}
	if err := v.BindEnv("KAFKA_GROUP_ID"); err != nil {
		return KafkaConfig{}, fmt.Errorf("failed to bind KAFKA_GROUP_ID: %w", err)
	}
	if err := v.BindEnv("KAFKA_DLQ_TOPIC"); err != nil {
		return KafkaConfig{}, fmt.Errorf("failed to bind KAFKA_DLQ_TOPIC: %w", err)
	}
	if err := v.BindEnv("KAFKA_MAX_RETRIES"); err != nil {
		return KafkaConfig{}, fmt.Errorf("failed to bind KAFKA_MAX_RETRIES: %w", err)
	}
	if err := v.BindEnv("KAFKA_RETRY_BASE_DELAY"); err != nil {
		return KafkaConfig{}, fmt.Errorf("failed to bind KAFKA_RETRY_BASE_DELAY: %w", err)
	}

	var config KafkaConfig
	if err := v.Unmarshal(&config); err != nil {
		return KafkaConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Viper doesn't directly unmarshal comma-separated strings to slices,
	// so assign brokers explicitly after Unmarshal to avoid it being overwritten.
	config.Brokers = strings.Split(v.GetString("KAFKA_BROKERS"), ",")

	return config, nil
}
