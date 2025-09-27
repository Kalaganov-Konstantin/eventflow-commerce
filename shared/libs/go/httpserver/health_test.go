package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandlers_HealthAndLive(t *testing.T) {
	h := NewHealthHandlers("order", nil)

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"health", h.Health},
		{"live", h.Live},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()

			tc.handler(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
			}

			var status HealthStatus
			if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if status.Status != "healthy" {
				t.Errorf("expected status 'healthy', got %q", status.Status)
			}
			if status.Service != "order" {
				t.Errorf("expected service 'order', got %q", status.Service)
			}
		})
	}
}

func TestHealthHandlers_Ready_AllChecksPass(t *testing.T) {
	checks := map[string]Check{
		"database": func(context.Context) error { return nil },
	}
	h := NewHealthHandlers("order", checks)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	h.Ready(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var status HealthStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if status.Status != "healthy" {
		t.Errorf("expected status 'healthy', got %q", status.Status)
	}
}

func TestHealthHandlers_Ready_FailingCheck(t *testing.T) {
	checks := map[string]Check{
		"database": func(context.Context) error { return errors.New("connection refused") },
	}
	h := NewHealthHandlers("order", checks)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	h.Ready(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	var status HealthStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if status.Status != "unhealthy" {
		t.Errorf("expected status 'unhealthy', got %q", status.Status)
	}
	if status.Details["database"] != "connection refused" {
		t.Errorf("expected failure detail for 'database', got %v", status.Details)
	}
}

func TestHealthHandlers_Register(t *testing.T) {
	h := NewHealthHandlers("order", nil)
	mux := http.NewServeMux()
	h.Register(mux)

	for _, path := range []string{"/health", "/health/live", "/health/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s: expected status %d, got %d", path, http.StatusOK, w.Code)
		}
	}
}
