package metrics

import (
	stderrors "errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
)

func TestDatabaseMetrics_Collect_ReportsPoolStatsAndOutboxBacklog(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(outboxPendingQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	collector := NewDatabaseMetrics(db, "order")

	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	names := map[string]bool{}
	for _, mf := range families {
		metric := mf.GetMetric()[0]
		if len(metric.GetLabel()) != 1 || metric.GetLabel()[0].GetValue() != "order" {
			t.Errorf("%s labels = %v, want service=order", mf.GetName(), metric.GetLabel())
		}
		names[mf.GetName()] = true

		if mf.GetName() == "outbox_backlog_size" && metric.GetGauge().GetValue() != 7 {
			t.Errorf("outbox_backlog_size = %v, want 7", metric.GetGauge().GetValue())
		}
	}

	for _, name := range []string{
		"db_connections_open", "db_connections_idle", "db_connections_in_use",
		"db_connections_wait_total", "outbox_backlog_size",
	} {
		if !names[name] {
			t.Errorf("missing metric %s", name)
		}
	}
	if len(names) != 5 {
		t.Errorf("collected %d distinct metrics, want 5", len(names))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDatabaseMetrics_Collect_SkipsOutboxBacklogOnQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(outboxPendingQuery)).WillReturnError(stderrors.New("db unavailable"))

	collector := NewDatabaseMetrics(db, "payment")

	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if len(families) != 4 {
		t.Fatalf("collected %d metric families, want 4 (outbox backlog skipped)", len(families))
	}
	for _, mf := range families {
		if mf.GetName() == "outbox_backlog_size" {
			t.Errorf("outbox_backlog_size reported despite the query failing")
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
