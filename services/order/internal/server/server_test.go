package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/cache"
	sharedConfig "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/database"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/httpserver"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
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

func TestServer_HealthEndpoints_KafkaReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "order", Version: "1.0.0"},
		Kafka:   sharedConfig.KafkaConfig{Brokers: []string{ln.Addr().String()}},
	}
	srv := New(Options{
		Config:  cfg,
		Logger:  zaptest.NewLogger(t),
		Metrics: prometheus.NewRegistry(),
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestServer_HealthEndpoints_KafkaUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "order", Version: "1.0.0"},
		Kafka:   sharedConfig.KafkaConfig{Brokers: []string{addr}},
	}
	srv := New(Options{
		Config:  cfg,
		Logger:  zaptest.NewLogger(t),
		Metrics: prometheus.NewRegistry(),
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusServiceUnavailable, w.Code, w.Body.String())
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

func TestNew_WithDatabase_RegistersOrderRoutes(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "order", Version: "1.0.0"},
	}
	srv := New(Options{
		Config:  cfg,
		Logger:  zaptest.NewLogger(t),
		Metrics: prometheus.NewRegistry(),
		DB:      &database.DB{DB: db},
	})

	// customerIDFromHeader rejects the request before it ever reaches the repository, so a bare
	// database handle with no query expectations is enough to prove the order routes are wired.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+uuidLikeSegment, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/orders/{id} with no DB wired = %d, want %d (missing X-User-ID)", w.Code, http.StatusUnauthorized)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	readyW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(readyW, readyReq)
	if readyW.Code != http.StatusOK {
		t.Errorf("/health/ready with a database configured = %d, want %d (body=%s)", readyW.Code, http.StatusOK, readyW.Body.String())
	}
}

const uuidLikeSegment = "11111111-1111-1111-1111-111111111111"

func TestNew_WithDatabaseAndRedis_RegistersCacheAndHealthCheck(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	defer func() { _ = redisClient.Close() }()

	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "order", Version: "1.0.0"},
	}
	registry := prometheus.NewRegistry()
	srv := New(Options{
		Config:       cfg,
		Logger:       zaptest.NewLogger(t),
		Metrics:      registry,
		DB:           &database.DB{DB: db},
		Redis:        &database.RedisClient{Client: redisClient},
		CacheMetrics: cache.NewMetrics(registry),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+uuidLikeSegment, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/orders/{id} = %d, want %d (missing X-User-ID)", w.Code, http.StatusUnauthorized)
	}

	// The redis client points at an address nothing listens on, so readiness must report it down.
	readyReq := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	readyW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(readyW, readyReq)
	if readyW.Code != http.StatusServiceUnavailable {
		t.Errorf("/health/ready with an unreachable redis = %d, want %d (body=%s)", readyW.Code, http.StatusServiceUnavailable, readyW.Body.String())
	}
}

func TestNew_DefaultMetricsRegisterer(t *testing.T) {
	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "order", Version: "1.0.0"},
	}
	srv := New(Options{Config: cfg, Logger: zaptest.NewLogger(t)})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRoutePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/health", "/health"},
		{"/api/v1/orders", "/api/v1/orders"},
		{"/api/v1/orders/11111111-1111-1111-1111-111111111111", "/api/v1/orders"},
		{"/api/v1/orders/11111111-1111-1111-1111-111111111111/items", "/api/v1/orders"},
	}

	for _, tt := range tests {
		if got := routePath(tt.path); got != tt.want {
			t.Errorf("routePath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
