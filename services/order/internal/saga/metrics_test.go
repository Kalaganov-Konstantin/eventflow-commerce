package saga

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetrics_RecordTransition(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry)

	m.RecordTransition(StateStarted, StateStockReserved)
	m.RecordTransition(StateStarted, StateStockReserved)
	m.RecordTransition(StateStockReserved, StateAwaitingPayment)

	if got := testutil.ToFloat64(m.transitions.WithLabelValues(string(StateStarted), string(StateStockReserved))); got != 2 {
		t.Errorf("started->stock_reserved total = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.transitions.WithLabelValues(string(StateStockReserved), string(StateAwaitingPayment))); got != 1 {
		t.Errorf("stock_reserved->awaiting_payment total = %v, want 1", got)
	}
}

func TestMetrics_RecordCompleted(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry)

	m.RecordCompleted(time.Now().Add(-2 * time.Second))

	if got := testutil.ToFloat64(m.completed); got != 1 {
		t.Errorf("completed total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.compensated); got != 0 {
		t.Errorf("compensated total = %v, want 0", got)
	}
	if count := testutil.CollectAndCount(m.duration); count != 1 {
		t.Errorf("expected 1 duration series, got %d", count)
	}
}

func TestMetrics_RecordCompensated(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry)

	m.RecordCompensated(time.Now().Add(-500 * time.Millisecond))

	if got := testutil.ToFloat64(m.compensated); got != 1 {
		t.Errorf("compensated total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.completed); got != 0 {
		t.Errorf("completed total = %v, want 0", got)
	}
	if count := testutil.CollectAndCount(m.duration); count != 1 {
		t.Errorf("expected 1 duration series, got %d", count)
	}
}

func TestNewMetrics_DuplicateRegistrationPanics(t *testing.T) {
	registry := prometheus.NewRegistry()
	NewMetrics(registry)

	defer func() {
		if recover() == nil {
			t.Fatal("expected registering saga metrics twice on the same registry to panic")
		}
	}()
	NewMetrics(registry)
}
