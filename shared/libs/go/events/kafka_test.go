package events

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// fakeReader is a substitute kafkaReader that serves a single preset message and records
// which messages were committed.
type fakeReader struct {
	message   kafka.Message
	committed []kafka.Message
}

func (f *fakeReader) FetchMessage(_ context.Context) (kafka.Message, error) {
	return f.message, nil
}

func (f *fakeReader) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	f.committed = append(f.committed, msgs...)
	return nil
}

func (f *fakeReader) Close() error { return nil }

// fakeWriter is a substitute kafkaWriter that either records written messages or fails.
type fakeWriter struct {
	written []kafka.Message
	err     error
}

func (f *fakeWriter) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	if f.err != nil {
		return f.err
	}
	f.written = append(f.written, msgs...)
	return nil
}

func (f *fakeWriter) Close() error { return nil }

func mustMarshalEvent(t *testing.T, event Event) []byte {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal(event) error = %v", err)
	}
	return data
}

func TestSubscriber_ProcessMessage_CommitsAfterHandlerSuccess(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	sub := &Subscriber{reader: reader, logger: zap.NewNop()}

	sub.processMessage(context.Background(), msg, func(Event) error { return nil })

	if len(reader.committed) != 1 {
		t.Fatalf("committed = %d messages, want 1", len(reader.committed))
	}
}

func TestSubscriber_ProcessMessage_CommitsAfterSuccessfulDLQWrite(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	dlq := &fakeWriter{}
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), dlqWriter: dlq}

	sub.processMessage(context.Background(), msg, func(Event) error { return errors.New("boom") })

	if len(dlq.written) != 1 {
		t.Fatalf("DLQ written = %d messages, want 1", len(dlq.written))
	}
	if len(reader.committed) != 1 {
		t.Fatalf("committed = %d messages, want 1", len(reader.committed))
	}
}

func TestSubscriber_ProcessMessage_DoesNotCommitWhenHandlerFailsAndDLQUnavailable(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	dlq := &fakeWriter{err: errors.New("dlq unreachable")}
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), dlqWriter: dlq}

	sub.processMessage(context.Background(), msg, func(Event) error { return errors.New("boom") })

	if len(reader.committed) != 0 {
		t.Fatalf("committed = %d messages, want 0 when handler fails and the DLQ is unavailable", len(reader.committed))
	}
}

func TestSubscriber_ProcessMessage_DoesNotCommitWhenDLQNotConfigured(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	sub := &Subscriber{reader: reader, logger: zap.NewNop()}

	sub.processMessage(context.Background(), msg, func(Event) error { return errors.New("boom") })

	if len(reader.committed) != 0 {
		t.Fatalf("committed = %d messages, want 0 when handler fails and no DLQ is configured", len(reader.committed))
	}
}

func TestLoadKafkaConfig_MultipleBrokers(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", "a:9092,b:9092")

	cfg, err := LoadKafkaConfig()
	if err != nil {
		t.Fatalf("LoadKafkaConfig() error = %v", err)
	}

	want := []string{"a:9092", "b:9092"}
	if len(cfg.Brokers) != len(want) {
		t.Fatalf("Brokers = %#v, want %#v", cfg.Brokers, want)
	}
	for i, broker := range want {
		if cfg.Brokers[i] != broker {
			t.Errorf("Brokers[%d] = %q, want %q", i, cfg.Brokers[i], broker)
		}
	}
}

func TestLoadKafkaConfig_DefaultsWithoutEnv(t *testing.T) {
	cfg, err := LoadKafkaConfig()
	if err != nil {
		t.Fatalf("LoadKafkaConfig() error = %v", err)
	}

	if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "localhost:9092" {
		t.Errorf("Brokers = %#v, want [localhost:9092]", cfg.Brokers)
	}
	if cfg.GroupID != "eventflow-service" {
		t.Errorf("GroupID = %q, want %q", cfg.GroupID, "eventflow-service")
	}
	if cfg.DLQTopic != "eventflow-dlq" {
		t.Errorf("DLQTopic = %q, want %q", cfg.DLQTopic, "eventflow-dlq")
	}
}

func TestLoadKafkaConfig_GroupIDAndDLQTopicFromEnv(t *testing.T) {
	t.Setenv("KAFKA_GROUP_ID", "order-service")
	t.Setenv("KAFKA_DLQ_TOPIC", "orders.events.dlq")

	cfg, err := LoadKafkaConfig()
	if err != nil {
		t.Fatalf("LoadKafkaConfig() error = %v", err)
	}

	if cfg.GroupID != "order-service" {
		t.Errorf("GroupID = %q, want %q", cfg.GroupID, "order-service")
	}
	if cfg.DLQTopic != "orders.events.dlq" {
		t.Errorf("DLQTopic = %q, want %q", cfg.DLQTopic, "orders.events.dlq")
	}
}
