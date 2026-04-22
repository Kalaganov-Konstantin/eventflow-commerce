package cache

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetrics_ObserveHit(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry)

	m.ObserveHit("product")
	m.ObserveHit("product")

	got := testutil.ToFloat64(m.hits.WithLabelValues("product"))
	if got != 2 {
		t.Errorf("hits total = %v, want 2", got)
	}
}

func TestMetrics_ObserveMiss(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry)

	m.ObserveMiss("order")

	got := testutil.ToFloat64(m.misses.WithLabelValues("order"))
	if got != 1 {
		t.Errorf("misses total = %v, want 1", got)
	}
}

func TestMetrics_ObserveError(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry)

	m.ObserveError("order")

	got := testutil.ToFloat64(m.errors.WithLabelValues("order"))
	if got != 1 {
		t.Errorf("errors total = %v, want 1", got)
	}
}

func TestMetrics_LabelsAreIndependentPerCacheName(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry)

	m.ObserveHit("product")
	m.ObserveHit("order")
	m.ObserveHit("order")

	if got := testutil.ToFloat64(m.hits.WithLabelValues("product")); got != 1 {
		t.Errorf("product hits = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.hits.WithLabelValues("order")); got != 2 {
		t.Errorf("order hits = %v, want 2", got)
	}
}

func TestNewMetrics_DuplicateRegistrationPanics(t *testing.T) {
	registry := prometheus.NewRegistry()
	NewMetrics(registry)

	defer func() {
		if recover() == nil {
			t.Fatal("expected registering cache metrics twice on the same registry to panic")
		}
	}()
	NewMetrics(registry)
}
