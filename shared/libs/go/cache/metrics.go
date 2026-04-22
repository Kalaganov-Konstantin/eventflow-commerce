package cache

import "github.com/prometheus/client_golang/prometheus"

// Metrics records cache hits, misses and errors, labeled by cache name.
type Metrics struct {
	hits   *prometheus.CounterVec
	misses *prometheus.CounterVec
	errors *prometheus.CounterVec
}

// NewMetrics registers cache hit, miss and error counters on registerer.
func NewMetrics(registerer prometheus.Registerer) *Metrics {
	labels := []string{"cache"}

	m := &Metrics{
		hits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_hits_total",
			Help: "Total number of cache reads that found a value.",
		}, labels),
		misses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_misses_total",
			Help: "Total number of cache reads that found no value.",
		}, labels),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_errors_total",
			Help: "Total number of cache operations that failed.",
		}, labels),
	}

	registerer.MustRegister(m.hits, m.misses, m.errors)
	return m
}

// ObserveHit increments the hit counter for cacheName.
func (m *Metrics) ObserveHit(cacheName string) {
	m.hits.WithLabelValues(cacheName).Inc()
}

// ObserveMiss increments the miss counter for cacheName.
func (m *Metrics) ObserveMiss(cacheName string) {
	m.misses.WithLabelValues(cacheName).Inc()
}

// ObserveError increments the error counter for cacheName.
func (m *Metrics) ObserveError(cacheName string) {
	m.errors.WithLabelValues(cacheName).Inc()
}
