package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/config"
	sharedConfig "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/httpserver"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap/zaptest"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "order", Version: "1.0.0"},
	}
	return New(Options{
		Config:  cfg,
		Logger:  zaptest.NewLogger(t),
		Metrics: prometheus.NewRegistry(),
	})
}

func TestNew(t *testing.T) {
	srv := newTestServer(t)

	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.Handler() == nil {
		t.Fatal("server handler not initialized")
	}
}

func TestServer_HealthEndpoints(t *testing.T) {
	srv := newTestServer(t)

	for _, path := range []string{"/health", "/health/live", "/health/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s: expected status %d, got %d", path, http.StatusOK, w.Code)
		}

		var status httpserver.HealthStatus
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatalf("%s: failed to parse response: %v", path, err)
		}
		if status.Service != "order" {
			t.Errorf("%s: expected service 'order', got %q", path, status.Service)
		}
	}
}

func TestServer_MetricsEndpoint(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("expected metrics body, got empty response")
	}
}

func TestServer_StartAndStop(t *testing.T) {
	srv := newTestServer(t)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	time.Sleep(10 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		t.Errorf("Start returned unexpected error: %v", err)
	}
}
