//go:build integration

package events

import (
	"context"
	stderrors "errors"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

const (
	defaultTestKafkaBroker = "localhost:9094"
	topicReadyTimeout      = 15 * time.Second
	messageWaitTimeout     = 20 * time.Second
)

// testKafkaBroker returns the integration test Kafka broker address, TEST_KAFKA_BROKER overriding
// the default docker-compose.test.yml listener.
func testKafkaBroker() string {
	if v := os.Getenv("TEST_KAFKA_BROKER"); v != "" {
		return v
	}
	return defaultTestKafkaBroker
}

// ensureTopic creates topic on the test broker and waits until it actually accepts writes. Kafka
// auto-creates topics on first access, but a produce request that arrives before creation finishes
// still fails with "unknown topic or partition", so the readiness check is a throwaway produce
// rather than a metadata lookup.
func ensureTopic(t *testing.T, topic string) {
	t.Helper()

	conn, err := kafka.Dial("tcp", testKafkaBroker())
	if err != nil {
		t.Fatalf("dial kafka broker: %v", err)
	}
	defer func() { _ = conn.Close() }()

	controller, err := conn.Controller()
	if err != nil {
		t.Fatalf("find kafka controller: %v", err)
	}

	controllerConn, err := kafka.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		t.Fatalf("dial kafka controller: %v", err)
	}
	defer func() { _ = controllerConn.Close() }()

	err = controllerConn.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     1,
		ReplicationFactor: 1,
	})
	if err != nil && !stderrors.Is(err, kafka.TopicAlreadyExists) {
		t.Fatalf("create topic %s: %v", topic, err)
	}

	deadline := time.Now().Add(topicReadyTimeout)
	for !canProduce(topic) {
		if time.Now().After(deadline) {
			t.Fatalf("topic %s did not become ready to accept writes in time", topic)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func canProduce(topic string) bool {
	writer := &kafka.Writer{
		Addr:     kafka.TCP(testKafkaBroker()),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
	}
	defer func() { _ = writer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return writer.WriteMessages(ctx, kafka.Message{Value: []byte("readiness-probe")}) == nil
}

// TestKafkaIntegration_PoisonMessageReachesDLQ drives a message that cannot be unmarshaled through
// the real reader and the real DLQ writer. A unit test cannot catch this path: kafka-go rejects a
// write only when both the writer and the message name a topic, and a fetched message always
// carries the topic it was read from.
func TestKafkaIntegration_PoisonMessageReachesDLQ(t *testing.T) {
	run := uuid.New().String()
	sourceTopic := "dlq-regression-" + run + ".events"
	dlqTopic := DLQTopic(sourceTopic)
	ensureTopic(t, sourceTopic)
	ensureTopic(t, dlqTopic)

	key := []byte("poison-" + run)
	writer := &kafka.Writer{
		Addr:     kafka.TCP(testKafkaBroker()),
		Topic:    sourceTopic,
		Balancer: &kafka.LeastBytes{},
	}
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), messageWaitTimeout)
	defer cancelWrite()
	if err := writer.WriteMessages(writeCtx, kafka.Message{Key: key, Value: []byte("not json")}); err != nil {
		t.Fatalf("publish poison message: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close producer: %v", err)
	}

	sub := NewSubscriber(KafkaConfig{
		Brokers:  []string{testKafkaBroker()},
		GroupID:  "dlq-regression-" + run,
		DLQTopic: dlqTopic,
	}, sourceTopic, zap.NewNop())
	t.Cleanup(func() { _ = sub.Close() })

	// NewSubscriber starts a fresh group at the latest offset, which would skip the message that
	// was published above. The DLQ writer it built is what this test is about, so only the reader
	// is replaced, with one that starts from the beginning of the topic.
	_ = sub.reader.Close()
	sub.reader = kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{testKafkaBroker()},
		Topic:       sourceTopic,
		GroupID:     "dlq-regression-reader-" + run,
		MinBytes:    1,
		MaxBytes:    10e6,
		MaxWait:     200 * time.Millisecond,
		StartOffset: kafka.FirstOffset,
	})

	ctx, cancel := context.WithTimeout(context.Background(), messageWaitTimeout)
	defer cancel()

	// ensureTopic left its readiness probe on the topic, so keep fetching until the poison message
	// itself comes round.
	for {
		msg, err := sub.reader.FetchMessage(ctx)
		if err != nil {
			t.Fatalf("fetch poison message from %s: %v", sourceTopic, err)
		}
		sub.processMessage(ctx, msg, func(context.Context, Event) error { return nil })
		if string(msg.Key) == string(key) {
			break
		}
	}

	dlqReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{testKafkaBroker()},
		Topic:     dlqTopic,
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	t.Cleanup(func() { _ = dlqReader.Close() })
	if err := dlqReader.SetOffset(kafka.FirstOffset); err != nil {
		t.Fatalf("set dlq reader offset: %v", err)
	}

	for {
		msg, err := dlqReader.ReadMessage(ctx)
		if err != nil {
			t.Fatalf("poison message never reached %s: %v", dlqTopic, err)
		}
		if string(msg.Key) != string(key) {
			continue
		}
		if string(msg.Value) != "not json" {
			t.Errorf("dlq message value = %q, want the original payload", msg.Value)
		}
		var errorType string
		for _, h := range msg.Headers {
			if h.Key == "errorType" {
				errorType = string(h.Value)
			}
		}
		if errorType != "unmarshal_error" {
			t.Errorf("dlq message errorType header = %q, want unmarshal_error", errorType)
		}
		return
	}
}
