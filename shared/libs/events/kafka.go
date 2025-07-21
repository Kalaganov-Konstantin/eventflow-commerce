package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type KafkaConfig struct {
	Brokers  []string `mapstructure:"KAFKA_BROKERS"`
	GroupID  string   `mapstructure:"KAFKA_GROUP_ID"`
	DLQTopic string   `mapstructure:"KAFKA_DLQ_TOPIC"`
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

type Publisher struct {
	writer *kafka.Writer
}

type Subscriber struct {
	reader    *kafka.Reader
	logger    *zap.Logger
	dlqWriter *kafka.Writer
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

	return &Subscriber{reader: reader, logger: logger, dlqWriter: dlqWriter}
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

func (s *Subscriber) Subscribe(ctx context.Context, handler func(Event) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			msg, err := s.reader.ReadMessage(ctx)
			if err != nil {
				s.logger.Error("Failed to read message from Kafka", zap.Error(err))
				continue
			}

			var event Event
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				s.logger.Error("Failed to unmarshal Kafka message", zap.Error(err), zap.ByteString("message", msg.Value))
				s.sendToDLQ(ctx, msg, "unmarshal_error")
				continue
			}

			if err := handler(event); err != nil {
				s.logger.Error("Failed to handle event", zap.Error(err), zap.String("event_id", event.ID))
				s.sendToDLQ(ctx, msg, "handler_error")
				continue
			}
		}
	}
}

func (s *Subscriber) sendToDLQ(ctx context.Context, msg kafka.Message, errorType string) {
	if s.dlqWriter == nil {
		s.logger.Warn("DLQ topic not configured. Message will be dropped.", zap.ByteString("key", msg.Key))
		return
	}

	msg.Headers = append(msg.Headers, kafka.Header{Key: "errorType", Value: []byte(errorType)})

	if err := s.dlqWriter.WriteMessages(ctx, msg); err != nil {
		s.logger.Error("Failed to send message to DLQ", zap.Error(err), zap.ByteString("key", msg.Key))
	}
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

	var config KafkaConfig
	// Viper doesn't directly unmarshal comma-separated strings to slices
	// So we read it as a string and then split it.
	brokersStr := v.GetString("KAFKA_BROKERS")
	config.Brokers = strings.Split(brokersStr, ",")

	if err := v.Unmarshal(&config); err != nil {
		return KafkaConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return config, nil
}
