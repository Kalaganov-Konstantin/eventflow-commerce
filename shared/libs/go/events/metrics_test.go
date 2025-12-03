package events

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestKafkaMetrics_ObservePublished(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)

	m.ObservePublished(OrdersTopic, EventTypeOrderCreated)
	m.ObservePublished(OrdersTopic, EventTypeOrderCreated)

	got := testutil.ToFloat64(m.published.WithLabelValues(OrdersTopic, EventTypeOrderCreated))
	if got != 2 {
		t.Errorf("published total = %v, want 2", got)
	}
}

func TestKafkaMetrics_ObserveConsumed(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)

	m.ObserveConsumed(OrdersTopic, EventTypeOrderCreated, 250*time.Millisecond)

	got := testutil.ToFloat64(m.consumed.WithLabelValues(OrdersTopic, EventTypeOrderCreated))
	if got != 1 {
		t.Errorf("consumed total = %v, want 1", got)
	}

	count := testutil.CollectAndCount(m.processingTime)
	if count != 1 {
		t.Errorf("expected 1 processing duration series, got %d", count)
	}
}

func TestKafkaMetrics_ObserveDLQ(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)

	m.ObserveDLQ(OrdersTopic, EventTypeOrderCreated)

	got := testutil.ToFloat64(m.dlq.WithLabelValues(OrdersTopic, EventTypeOrderCreated))
	if got != 1 {
		t.Errorf("dlq total = %v, want 1", got)
	}
}

func TestKafkaMetrics_SetOutboxPending(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewKafkaMetrics(registry)

	m.SetOutboxPending(7)

	if got := testutil.ToFloat64(m.outboxPending); got != 7 {
		t.Errorf("outbox pending = %v, want 7", got)
	}

	m.SetOutboxPending(3)
	if got := testutil.ToFloat64(m.outboxPending); got != 3 {
		t.Errorf("outbox pending = %v, want 3", got)
	}
}

func TestNewKafkaMetrics_DuplicateRegistrationPanics(t *testing.T) {
	registry := prometheus.NewRegistry()
	NewKafkaMetrics(registry)

	defer func() {
		if recover() == nil {
			t.Fatal("expected registering Kafka metrics twice on the same registry to panic")
		}
	}()
	NewKafkaMetrics(registry)
}
