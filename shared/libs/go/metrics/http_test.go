package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHTTPMetrics_Middleware(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewHTTPMetrics(registry, "order")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := m.Middleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	got := testutil.ToFloat64(m.requestsTotal.WithLabelValues("GET", "/orders/{id}", "200"))
	if got != 1 {
		t.Errorf("expected requests_total 1, got %v", got)
	}

	count := testutil.CollectAndCount(m.requestDuration)
	if count != 1 {
		t.Errorf("expected 1 duration series, got %d", count)
	}
}

func TestHTTPMetrics_Middleware_UnmatchedRoute(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewHTTPMetrics(registry, "order")

	mux := http.NewServeMux()
	handler := m.Middleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	got := testutil.ToFloat64(m.requestsTotal.WithLabelValues("GET", "unmatched", "404"))
	if got != 1 {
		t.Errorf("expected requests_total 1 for unmatched route, got %v", got)
	}
}

func TestNewHTTPMetrics_DuplicateRegistrationPanics(t *testing.T) {
	registry := prometheus.NewRegistry()
	NewHTTPMetrics(registry, "order")

	defer func() {
		if recover() == nil {
			t.Fatal("expected registering the same service metrics twice to panic")
		}
	}()
	NewHTTPMetrics(registry, "order")
}
