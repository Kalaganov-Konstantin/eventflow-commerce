package events

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// KafkaMetrics records Kafka publish and consume activity plus the outbox backlog size.
type KafkaMetrics struct {
	published      *prometheus.CounterVec
	consumed       *prometheus.CounterVec
	dlq            *prometheus.CounterVec
	processingTime *prometheus.HistogramVec
	outboxPending  prometheus.Gauge
}

// NewKafkaMetrics registers Kafka event counters, a handling duration histogram and an outbox
// backlog gauge on registerer.
func NewKafkaMetrics(registerer prometheus.Registerer) *KafkaMetrics {
	labels := []string{"topic", "event_type"}

	m := &KafkaMetrics{
		published: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kafka_events_published_total",
			Help: "Total number of events published to Kafka.",
		}, labels),
		consumed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kafka_events_consumed_total",
			Help: "Total number of events consumed from Kafka.",
		}, labels),
		dlq: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kafka_events_dlq_total",
			Help: "Total number of events sent to a dead letter queue.",
		}, labels),
		processingTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kafka_event_processing_duration_seconds",
			Help:    "Duration of Kafka event handling in seconds.",
			Buckets: prometheus.DefBuckets,
		}, labels),
		outboxPending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_pending_messages",
			Help: "Number of outbox messages not yet published.",
		}),
	}

	registerer.MustRegister(m.published, m.consumed, m.dlq, m.processingTime, m.outboxPending)
	return m
}

// ObservePublished increments the published counter for topic and eventType.
func (m *KafkaMetrics) ObservePublished(topic, eventType string) {
	m.published.WithLabelValues(topic, eventType).Inc()
}

// ObserveConsumed increments the consumed counter and records handling duration for topic and
// eventType.
func (m *KafkaMetrics) ObserveConsumed(topic, eventType string, duration time.Duration) {
	m.consumed.WithLabelValues(topic, eventType).Inc()
	m.processingTime.WithLabelValues(topic, eventType).Observe(duration.Seconds())
}

// ObserveDLQ increments the DLQ counter for topic and eventType.
func (m *KafkaMetrics) ObserveDLQ(topic, eventType string) {
	m.dlq.WithLabelValues(topic, eventType).Inc()
}

// SetOutboxPending sets the outbox backlog gauge to count.
func (m *KafkaMetrics) SetOutboxPending(count float64) {
	m.outboxPending.Set(count)
}
