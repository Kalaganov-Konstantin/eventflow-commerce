package events

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// fakeReader is a substitute kafkaReader that serves a single preset message and records
// which messages were committed.
type fakeReader struct {
	message   kafka.Message
	committed []kafka.Message
	commitErr error
}

func (f *fakeReader) FetchMessage(_ context.Context) (kafka.Message, error) {
	return f.message, nil
}

func (f *fakeReader) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	if f.commitErr != nil {
		return f.commitErr
	}
	f.committed = append(f.committed, msgs...)
	return nil
}

func (f *fakeReader) Close() error { return nil }

// loopReader serves the same message once, then blocks until ctx is cancelled, so Subscribe's
// fetch loop can be exercised without busy-spinning while the test waits to cancel it. It is
// only ever driven by Subscribe's own goroutine, so the fetched flag needs no synchronization.
type loopReader struct {
	message kafka.Message
	fetched bool
}

func (l *loopReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if !l.fetched {
		l.fetched = true
		return l.message, nil
	}
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (l *loopReader) CommitMessages(_ context.Context, _ ...kafka.Message) error { return nil }

func (l *loopReader) Close() error { return nil }

// flakyThenBlockingReader fails the first fetch with a transient error, succeeds once on the
// second, then blocks until ctx is cancelled. It exercises Subscribe's retry-after-fetch-error
// path without a busy loop; only ever driven by Subscribe's own goroutine.
type flakyThenBlockingReader struct {
	message  kafka.Message
	attempts int
}

func (f *flakyThenBlockingReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	f.attempts++
	switch f.attempts {
	case 1:
		return kafka.Message{}, errors.New("temporary broker error")
	case 2:
		return f.message, nil
	default:
		<-ctx.Done()
		return kafka.Message{}, ctx.Err()
	}
}

func (f *flakyThenBlockingReader) CommitMessages(_ context.Context, _ ...kafka.Message) error {
	return nil
}

func (f *flakyThenBlockingReader) Close() error { return nil }

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

func TestPublisher_Publish_KeysByAggregateIDWhenSet(t *testing.T) {
	writer := &fakeWriter{}
	pub := &Publisher{writer: writer}

	err := pub.Publish(context.Background(), OrdersTopic, Event{
		ID:          "evt-1",
		AggregateID: "order-42",
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if len(writer.written) != 1 {
		t.Fatalf("written = %d messages, want 1", len(writer.written))
	}
	if got := string(writer.written[0].Key); got != "order-42" {
		t.Errorf("message key = %q, want %q", got, "order-42")
	}
}

func TestPublisher_Publish_KeysByEventIDWhenAggregateIDUnset(t *testing.T) {
	writer := &fakeWriter{}
	pub := &Publisher{writer: writer}

	err := pub.Publish(context.Background(), OrdersTopic, Event{ID: "evt-1"})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if len(writer.written) != 1 {
		t.Fatalf("written = %d messages, want 1", len(writer.written))
	}
	if got := string(writer.written[0].Key); got != "evt-1" {
		t.Errorf("message key = %q, want %q", got, "evt-1")
	}
}

func TestPublisher_Publish_GeneratesIDTimestampAndVersionWhenUnset(t *testing.T) {
	writer := &fakeWriter{}
	pub := &Publisher{writer: writer}

	before := time.Now()
	if err := pub.Publish(context.Background(), OrdersTopic, Event{}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	var sent Event
	if err := json.Unmarshal(writer.written[0].Value, &sent); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if sent.ID == "" {
		t.Error("event ID was not generated")
	}
	if sent.Version != "1.0" {
		t.Errorf("Version = %q, want %q", sent.Version, "1.0")
	}
	if sent.Timestamp.Before(before) {
		t.Errorf("Timestamp = %v, want at or after %v", sent.Timestamp, before)
	}
}

func TestPublisher_Publish_IncludesCorrelationIDHeaderWhenSet(t *testing.T) {
	writer := &fakeWriter{}
	pub := &Publisher{writer: writer}

	err := pub.Publish(context.Background(), OrdersTopic, Event{ID: "evt-1", CorrelationID: "corr-1"})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	var got string
	for _, h := range writer.written[0].Headers {
		if h.Key == "correlationId" {
			got = string(h.Value)
		}
	}
	if got != "corr-1" {
		t.Errorf("correlationId header = %q, want %q", got, "corr-1")
	}
}

func TestPublisher_Publish_ReturnsWriteError(t *testing.T) {
	writer := &fakeWriter{err: errors.New("broker unreachable")}
	pub := &Publisher{writer: writer}

	if err := pub.Publish(context.Background(), OrdersTopic, Event{ID: "evt-1"}); err == nil {
		t.Fatal("Publish() error = nil, want error when the underlying write fails")
	}
}

func TestPublisher_Publish_ReturnsMarshalError(t *testing.T) {
	pub := &Publisher{writer: &fakeWriter{}}

	event := Event{ID: "evt-1", Data: map[string]interface{}{"bad": make(chan int)}}
	if err := pub.Publish(context.Background(), OrdersTopic, event); err == nil {
		t.Fatal("Publish() error = nil, want a marshal error for an unmarshalable data value")
	}
}

func TestSubscriber_ProcessMessage_CommitsAfterHandlerSuccess(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	sub := &Subscriber{reader: reader, logger: zap.NewNop()}

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return nil })

	if len(reader.committed) != 1 {
		t.Fatalf("committed = %d messages, want 1", len(reader.committed))
	}
}

func TestSubscriber_ProcessMessage_CommitsAfterSuccessfulDLQWrite(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	dlq := &fakeWriter{}
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), dlqWriter: dlq}

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return errors.New("boom") })

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

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return errors.New("boom") })

	if len(reader.committed) != 0 {
		t.Fatalf("committed = %d messages, want 0 when handler fails and the DLQ is unavailable", len(reader.committed))
	}
}

func TestSubscriber_ProcessMessage_DoesNotCommitWhenDLQNotConfigured(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	sub := &Subscriber{reader: reader, logger: zap.NewNop()}

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return errors.New("boom") })

	if len(reader.committed) != 0 {
		t.Fatalf("committed = %d messages, want 0 when handler fails and no DLQ is configured", len(reader.committed))
	}
}

func TestSubscriber_ProcessMessage_RetriesBeforeGivingUp(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	dlq := &fakeWriter{}
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), dlqWriter: dlq, maxRetries: 3, retryBaseDelay: time.Millisecond}

	var calls int
	sub.processMessage(context.Background(), msg, func(context.Context, Event) error {
		calls++
		if calls < 3 {
			return errors.New("transient failure")
		}
		return nil
	})

	if calls != 3 {
		t.Fatalf("handler called %d times, want 3", calls)
	}
	if len(dlq.written) != 0 {
		t.Fatalf("DLQ written = %d messages, want 0 when the handler eventually succeeds", len(dlq.written))
	}
	if len(reader.committed) != 1 {
		t.Fatalf("committed = %d messages, want 1", len(reader.committed))
	}
}

func TestSubscriber_ProcessMessage_StopsRetryingWhenContextCancelled(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	dlq := &fakeWriter{}
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), dlqWriter: dlq, maxRetries: 3, retryBaseDelay: time.Hour}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls int
	sub.processMessage(ctx, msg, func(context.Context, Event) error {
		calls++
		return errors.New("boom")
	})

	if calls != 1 {
		t.Fatalf("handler called %d times, want 1 (no retry once the context is cancelled)", calls)
	}
	if len(dlq.written) != 1 {
		t.Fatalf("DLQ written = %d messages, want 1", len(dlq.written))
	}
}

func TestSubscriber_ProcessMessage_LogsWhenCommitFails(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg, commitErr: errors.New("commit failed")}
	sub := &Subscriber{reader: reader, logger: zap.NewNop()}

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return nil })

	if len(reader.committed) != 0 {
		t.Fatalf("committed = %d messages, want 0 when the commit call itself fails", len(reader.committed))
	}
}

func TestSubscriber_ProcessMessage_SendsToDLQAfterRetriesExhausted(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	dlq := &fakeWriter{}
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), dlqWriter: dlq, maxRetries: 2, retryBaseDelay: time.Millisecond}

	var calls int
	sub.processMessage(context.Background(), msg, func(context.Context, Event) error {
		calls++
		return errors.New("permanent failure")
	})

	if calls != 3 { // initial attempt plus 2 retries
		t.Fatalf("handler called %d times, want 3", calls)
	}
	if len(dlq.written) != 1 {
		t.Fatalf("DLQ written = %d messages, want 1", len(dlq.written))
	}
}

func TestPublisher_Publish_RecordsPublishedMetric(t *testing.T) {
	writer := &fakeWriter{}
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)
	pub := &Publisher{writer: writer, metrics: m}

	err := pub.Publish(context.Background(), OrdersTopic, Event{ID: "evt-1", Type: "order.created"})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if got := testutil.ToFloat64(m.published.WithLabelValues(OrdersTopic, "order.created")); got != 1 {
		t.Errorf("published total = %v, want 1", got)
	}
}

func TestPublisher_Publish_NoMetricsConfiguredDoesNotPanic(t *testing.T) {
	pub := &Publisher{writer: &fakeWriter{}}

	if err := pub.Publish(context.Background(), OrdersTopic, Event{ID: "evt-1"}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
}

func TestPublisher_SetMetrics_WiresPublishObservations(t *testing.T) {
	writer := &fakeWriter{}
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)
	pub := &Publisher{writer: writer}
	pub.SetMetrics(m)

	err := pub.Publish(context.Background(), OrdersTopic, Event{ID: "evt-1", Type: "order.created"})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if got := testutil.ToFloat64(m.published.WithLabelValues(OrdersTopic, "order.created")); got != 1 {
		t.Errorf("published total = %v, want 1", got)
	}
}

func TestSubscriber_SetMetrics_WiresConsumedObservations(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), topic: OrdersTopic}
	sub.SetMetrics(m)

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return nil })

	if got := testutil.ToFloat64(m.consumed.WithLabelValues(OrdersTopic, "order.created")); got != 1 {
		t.Errorf("consumed total = %v, want 1", got)
	}
}

func TestSubscriber_ProcessMessage_RecordsConsumedMetricOnSuccess(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), topic: OrdersTopic, metrics: m}

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return nil })

	if got := testutil.ToFloat64(m.consumed.WithLabelValues(OrdersTopic, "order.created")); got != 1 {
		t.Errorf("consumed total = %v, want 1", got)
	}
}

func TestSubscriber_ProcessMessage_RecordsDLQMetricOnHandlerFailure(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	dlq := &fakeWriter{}
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), topic: OrdersTopic, dlqWriter: dlq, metrics: m}

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return errors.New("boom") })

	if got := testutil.ToFloat64(m.dlq.WithLabelValues(OrdersTopic, "order.created")); got != 1 {
		t.Errorf("dlq total = %v, want 1", got)
	}
}

func TestSubscriber_ProcessMessage_RecordsDLQMetricWithUnknownTypeOnUnmarshalFailure(t *testing.T) {
	msg := kafka.Message{Value: []byte("not json")}
	reader := &fakeReader{message: msg}
	dlq := &fakeWriter{}
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), topic: OrdersTopic, dlqWriter: dlq, metrics: m}

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return nil })

	if got := testutil.ToFloat64(m.dlq.WithLabelValues(OrdersTopic, "unknown")); got != 1 {
		t.Errorf("dlq total = %v, want 1", got)
	}
}

func TestSubscriber_ProcessMessage_NoMetricsConfiguredDoesNotPanic(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &fakeReader{message: msg}
	sub := &Subscriber{reader: reader, logger: zap.NewNop(), topic: OrdersTopic}

	sub.processMessage(context.Background(), msg, func(context.Context, Event) error { return nil })

	if len(reader.committed) != 1 {
		t.Fatalf("committed = %d messages, want 1", len(reader.committed))
	}
}

func TestSubscriber_Subscribe_ProcessesMessagesUntilContextCancelled(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &loopReader{message: msg}
	sub := &Subscriber{reader: reader, logger: zap.NewNop()}

	ctx, cancel := context.WithCancel(context.Background())
	handled := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- sub.Subscribe(ctx, func(context.Context, Event) error {
			close(handled)
			return nil
		})
	}()

	<-handled
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Subscribe() error = %v, want context.Canceled", err)
	}
}

func TestSubscriber_Subscribe_ContinuesAfterTransientFetchError(t *testing.T) {
	msg := kafka.Message{Value: mustMarshalEvent(t, Event{ID: "evt-1", Type: "order.created"})}
	reader := &flakyThenBlockingReader{message: msg}
	sub := &Subscriber{reader: reader, logger: zap.NewNop()}

	ctx, cancel := context.WithCancel(context.Background())
	handled := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- sub.Subscribe(ctx, func(context.Context, Event) error {
			close(handled)
			return nil
		})
	}()

	<-handled
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Subscribe() error = %v, want context.Canceled", err)
	}
	if reader.attempts < 2 {
		t.Errorf("attempts = %d, want at least 2 fetch calls", reader.attempts)
	}
}

func TestNewPublisher_ConstructsWriterAndCloses(t *testing.T) {
	pub := NewPublisher(KafkaConfig{Brokers: []string{"127.0.0.1:1"}})

	if err := pub.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewSubscriber_AppliesDefaultsWhenConfigZero(t *testing.T) {
	sub := NewSubscriber(KafkaConfig{Brokers: []string{"127.0.0.1:1"}}, OrdersTopic, zap.NewNop())

	if sub.maxRetries != defaultMaxRetries {
		t.Errorf("maxRetries = %d, want %d", sub.maxRetries, defaultMaxRetries)
	}
	if sub.retryBaseDelay != defaultRetryBaseDelay {
		t.Errorf("retryBaseDelay = %v, want %v", sub.retryBaseDelay, defaultRetryBaseDelay)
	}

	// Regression: a subscriber built without a DLQ topic used to panic here, because the nil
	// *kafka.Writer stored in the interface field still read as non nil.
	if err := sub.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSubscriber_Close_NoDLQWriterConfigured(t *testing.T) {
	sub := &Subscriber{reader: &fakeReader{}}

	if err := sub.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewSubscriber_UsesConfiguredRetrySettingsAndDLQTopic(t *testing.T) {
	cfg := KafkaConfig{
		Brokers:        []string{"127.0.0.1:1"},
		DLQTopic:       "orders.events.dlq",
		MaxRetries:     7,
		RetryBaseDelay: 250 * time.Millisecond,
	}
	sub := NewSubscriber(cfg, OrdersTopic, zap.NewNop())

	if sub.maxRetries != 7 {
		t.Errorf("maxRetries = %d, want 7", sub.maxRetries)
	}
	if sub.retryBaseDelay != 250*time.Millisecond {
		t.Errorf("retryBaseDelay = %v, want 250ms", sub.retryBaseDelay)
	}
	if sub.dlqWriter == nil {
		t.Fatal("dlqWriter should be set when a DLQ topic is configured")
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestHealthy_NoBrokersConfigured(t *testing.T) {
	if err := Healthy(context.Background(), nil); err == nil {
		t.Fatal("Healthy() error = nil, want error")
	}
}

func TestHealthy_ReachableBroker(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	if err := Healthy(context.Background(), []string{ln.Addr().String()}); err != nil {
		t.Fatalf("Healthy() error = %v", err)
	}
}

func TestHealthy_UnreachableBroker(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := Healthy(ctx, []string{addr}); err == nil {
		t.Fatal("Healthy() error = nil, want error")
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

func TestLoadKafkaConfig_MaxRetriesAndRetryBaseDelayFromEnv(t *testing.T) {
	t.Setenv("KAFKA_MAX_RETRIES", "5")
	t.Setenv("KAFKA_RETRY_BASE_DELAY", "250ms")

	cfg, err := LoadKafkaConfig()
	if err != nil {
		t.Fatalf("LoadKafkaConfig() error = %v", err)
	}

	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", cfg.MaxRetries)
	}
	if cfg.RetryBaseDelay != 250*time.Millisecond {
		t.Errorf("RetryBaseDelay = %v, want 250ms", cfg.RetryBaseDelay)
	}
}

func TestLoadKafkaConfig_ReturnsErrorForUnparsableEnvValue(t *testing.T) {
	t.Setenv("KAFKA_MAX_RETRIES", "not-an-int")

	if _, err := LoadKafkaConfig(); err == nil {
		t.Fatal("LoadKafkaConfig() error = nil, want error for a non-numeric KAFKA_MAX_RETRIES")
	}
}
