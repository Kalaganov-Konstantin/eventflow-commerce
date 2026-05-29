package metrics

import (
	"context"
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"
)

// outboxPendingQuery counts outbox rows not yet published. Every service's outbox table shares
// this schema, so the same query fits order, payment and inventory.
const outboxPendingQuery = `SELECT COUNT(*) FROM outbox_messages WHERE published_at IS NULL`

// DatabaseMetrics reports connection pool and outbox backlog stats for a service's database,
// read at scrape time instead of polled into background gauges.
type DatabaseMetrics struct {
	db *sql.DB

	openConnections  *prometheus.Desc
	idleConnections  *prometheus.Desc
	inUseConnections *prometheus.Desc
	waitTotal        *prometheus.Desc
	outboxBacklog    *prometheus.Desc
}

// NewDatabaseMetrics builds a DatabaseMetrics collector for db, labelling every metric with
// service. Register it with a prometheus.Registerer to have it scraped.
func NewDatabaseMetrics(db *sql.DB, service string) *DatabaseMetrics {
	labels := prometheus.Labels{"service": service}

	return &DatabaseMetrics{
		db: db,
		openConnections: prometheus.NewDesc(
			"db_connections_open",
			"Established connections to the database, both in use and idle.",
			nil, labels,
		),
		idleConnections: prometheus.NewDesc(
			"db_connections_idle",
			"Idle connections in the pool.",
			nil, labels,
		),
		inUseConnections: prometheus.NewDesc(
			"db_connections_in_use",
			"Connections currently in use.",
			nil, labels,
		),
		waitTotal: prometheus.NewDesc(
			"db_connections_wait_total",
			"Total number of connections a caller has waited for.",
			nil, labels,
		),
		outboxBacklog: prometheus.NewDesc(
			"outbox_backlog_size",
			"Number of outbox messages not yet published.",
			nil, labels,
		),
	}
}

// Describe sends every metric descriptor this collector can report.
func (m *DatabaseMetrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.openConnections
	ch <- m.idleConnections
	ch <- m.inUseConnections
	ch <- m.waitTotal
	ch <- m.outboxBacklog
}

// Collect reports the current connection pool stats and outbox backlog. A failed outbox count
// query is skipped rather than reported as a scrape error, since the pool stats are still valid.
func (m *DatabaseMetrics) Collect(ch chan<- prometheus.Metric) {
	stats := m.db.Stats()
	ch <- prometheus.MustNewConstMetric(m.openConnections, prometheus.GaugeValue, float64(stats.OpenConnections))
	ch <- prometheus.MustNewConstMetric(m.idleConnections, prometheus.GaugeValue, float64(stats.Idle))
	ch <- prometheus.MustNewConstMetric(m.inUseConnections, prometheus.GaugeValue, float64(stats.InUse))
	ch <- prometheus.MustNewConstMetric(m.waitTotal, prometheus.CounterValue, float64(stats.WaitCount))

	var pending float64
	if err := m.db.QueryRowContext(context.Background(), outboxPendingQuery).Scan(&pending); err == nil {
		ch <- prometheus.MustNewConstMetric(m.outboxBacklog, prometheus.GaugeValue, pending)
	}
}
