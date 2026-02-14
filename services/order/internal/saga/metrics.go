package saga

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics records order saga outcomes, per-state transitions and the time each saga takes to
// reach a terminal state.
type Metrics struct {
	completed   prometheus.Counter
	compensated prometheus.Counter
	transitions *prometheus.CounterVec
	duration    prometheus.Histogram
}

// NewMetrics registers order saga counters and a completion duration histogram on registerer.
func NewMetrics(registerer prometheus.Registerer) *Metrics {
	m := &Metrics{
		completed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "order_saga_completed_total",
			Help: "Total number of order sagas that reached the completed state.",
		}),
		compensated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "order_saga_compensated_total",
			Help: "Total number of order sagas that reached the compensated state.",
		}),
		transitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "order_saga_transitions_total",
			Help: "Total number of order saga state transitions.",
		}, []string{"from", "to"}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "order_saga_duration_seconds",
			Help:    "Duration from order creation to the saga reaching a terminal state, in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
	}

	registerer.MustRegister(m.completed, m.compensated, m.transitions, m.duration)
	return m
}

// RecordTransition increments the transition counter for a from-to move.
func (m *Metrics) RecordTransition(from, to State) {
	m.transitions.WithLabelValues(string(from), string(to)).Inc()
}

// RecordCompleted records a saga that reached the completed state, createdAt marking when its
// order was created.
func (m *Metrics) RecordCompleted(createdAt time.Time) {
	m.completed.Inc()
	m.duration.Observe(time.Since(createdAt).Seconds())
}

// RecordCompensated records a saga that reached the compensated state, createdAt marking when its
// order was created.
func (m *Metrics) RecordCompensated(createdAt time.Time) {
	m.compensated.Inc()
	m.duration.Observe(time.Since(createdAt).Seconds())
}
