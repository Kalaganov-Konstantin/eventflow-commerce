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
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/cache"
	sharedConfig "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/database"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/httpserver"
	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap/zaptest"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "inventory", Version: "1.0.0"},
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

func TestNew_DefaultRegisterer(t *testing.T) {
	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "inventory", Version: "1.0.0"},
	}
	srv := New(Options{Config: cfg, Logger: zaptest.NewLogger(t)})

	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.Handler() == nil {
		t.Fatal("server handler not initialized")
	}
}

func TestNew_WithDatabaseAndRedis(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "inventory", Version: "1.0.0"},
	}
	srv := New(Options{
		Config:       cfg,
		Logger:       zaptest.NewLogger(t),
		Metrics:      prometheus.NewRegistry(),
		DB:           &database.DB{DB: db},
		Redis:        &database.RedisClient{Client: client},
		CacheMetrics: cache.NewMetrics(prometheus.NewRegistry()),
	})

	for _, path := range []string{"/api/v1/products", "/api/v1/products/123", "/api/v1/inventory/123"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code == http.StatusNotFound {
			t.Errorf("%s: expected the route to be registered, got 404", path)
		}
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
		if status.Service != "inventory" {
			t.Errorf("%s: expected service 'inventory', got %q", path, status.Service)
		}
	}
}

func TestServer_HealthEndpoints_RedisReachable(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "inventory", Version: "1.0.0"},
	}
	srv := New(Options{
		Config:  cfg,
		Logger:  zaptest.NewLogger(t),
		Metrics: prometheus.NewRegistry(),
		Redis:   &database.RedisClient{Client: client},
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestServer_HealthEndpoints_RedisUnreachable(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = client.Close() })

	cfg := &config.Config{
		Server:  sharedConfig.ServerConfig{Host: "127.0.0.1", Port: "0"},
		Service: sharedConfig.ServiceConfig{Name: "inventory", Version: "1.0.0"},
	}
	srv := New(Options{
		Config:  cfg,
		Logger:  zaptest.NewLogger(t),
		Metrics: prometheus.NewRegistry(),
		Redis:   &database.RedisClient{Client: client},
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusServiceUnavailable, w.Code, w.Body.String())
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
		Service: sharedConfig.ServiceConfig{Name: "inventory", Version: "1.0.0"},
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
		Service: sharedConfig.ServiceConfig{Name: "inventory", Version: "1.0.0"},
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

func TestRoutePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/health", want: "/health"},
		{path: "/api/v1/products", want: "/api/v1/products"},
		{path: "/api/v1/products/123/sub/deep", want: "/api/v1/products"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := routePath(tt.path); got != tt.want {
				t.Errorf("routePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
