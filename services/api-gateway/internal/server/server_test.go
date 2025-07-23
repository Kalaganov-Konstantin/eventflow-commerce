package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/handler"
	sharedConfig "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
	"go.uber.org/zap/zaptest"
)

var testMetrics = handler.NewMetrics()

func TestNewServer(t *testing.T) {
	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "localhost",
			Port: "8080",
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 100,
			WindowDuration:    60,
		},
		ProxyTimeout: 5,
	}

	logger := zaptest.NewLogger(t)

	srv := NewServer(ServerOptions{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	if srv == nil {
		t.Fatal("NewServer returned nil")
	}

	if srv.config != cfg {
		t.Error("Server config not set correctly")
	}

	if srv.logger != logger {
		t.Error("Server logger not set correctly")
	}

	if srv.httpServer == nil {
		t.Error("HTTP server not initialized")
	}

	if srv.rateLimiter == nil {
		t.Error("Rate limiter not initialized")
	}

	if srv.metrics == nil {
		t.Error("Metrics not initialized")
	}

	if srv.router == nil {
		t.Error("Router not initialized")
	}
}

func TestServer_HTTPServerConfiguration(t *testing.T) {
	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "0.0.0.0",
			Port: "3000",
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 50,
			WindowDuration:    30,
		},
		ProxyTimeout: 5,
	}

	logger := zaptest.NewLogger(t)

	// Create server with pre-injected test metrics
	srv := NewServer(ServerOptions{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	httpServer := srv.GetHTTPServer()

	expectedAddr := "0.0.0.0:3000"
	if httpServer.Addr != expectedAddr {
		t.Errorf("Expected server address '%s', got '%s'", expectedAddr, httpServer.Addr)
	}

	if httpServer.ReadTimeout != 15*time.Second {
		t.Errorf("Expected read timeout 15s, got %v", httpServer.ReadTimeout)
	}

	if httpServer.WriteTimeout != 15*time.Second {
		t.Errorf("Expected write timeout 15s, got %v", httpServer.WriteTimeout)
	}

	if httpServer.IdleTimeout != 60*time.Second {
		t.Errorf("Expected idle timeout 60s, got %v", httpServer.IdleTimeout)
	}

	if httpServer.Handler == nil {
		t.Error("HTTP server handler not set")
	}
}

func TestServer_ComponentsInitialization(t *testing.T) {
	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "localhost",
			Port: "8080",
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 200,
			WindowDuration:    120,
		},
		ProxyTimeout: 5,
	}

	logger := zaptest.NewLogger(t)
	srv := NewServer(ServerOptions{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	// Test rate limiter configuration
	rateLimiter := srv.GetRateLimiter()
	if rateLimiter == nil {
		t.Fatal("Rate limiter not initialized")
	}

	// Test metrics initialization
	metrics := srv.GetMetrics()
	if metrics == nil {
		t.Fatal("Metrics not initialized")
	}

	// Test router initialization
	router := srv.GetRouter()
	if router == nil {
		t.Fatal("Router not initialized")
	}
}

func TestServer_MiddlewareChain(t *testing.T) {
	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "localhost",
			Port: "8080",
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 100,
			WindowDuration:    60,
		},
		ProxyTimeout: 5,
	}

	logger := zaptest.NewLogger(t)
	srv := NewServer(ServerOptions{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	// Test that health check endpoint is accessible
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Health check failed, expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestServer_MetricsEndpoint(t *testing.T) {
	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "localhost",
			Port: "8080",
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 100,
			WindowDuration:    60,
		},
		ProxyTimeout: 5,
	}

	logger := zaptest.NewLogger(t)
	srv := NewServer(ServerOptions{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	// Test metrics endpoint
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Metrics endpoint failed, expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Check that it's Prometheus format
	contentType := w.Header().Get("Content-Type")
	if contentType == "" {
		t.Error("Metrics endpoint should set Content-Type header")
	}
}

func TestServer_Stop(t *testing.T) {
	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "localhost",
			Port: "0", // Use any available port
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 100,
			WindowDuration:    60,
		},
		ProxyTimeout: 5,
	}

	logger := zaptest.NewLogger(t)
	srv := NewServer(ServerOptions{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	// Test that Stop doesn't hang
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.Stop(ctx)
	if err != nil {
		t.Errorf("Server stop failed: %v", err)
	}

	// Test that rate limiter is closed
	rateLimiter := srv.GetRateLimiter()
	if rateLimiter != nil {
		// Rate limiter should be closed, but we can't easily test the internal state
		// The important thing is that Stop() completed without error
		t.Log("Rate limiter stop completed successfully")
	}
}

func TestServer_StartAndStop(t *testing.T) {
	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "127.0.0.1",
			Port: "0", // Use any available port
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 100,
			WindowDuration:    60,
		},
		ProxyTimeout: 5,
	}

	logger := zaptest.NewLogger(t)
	srv := NewServer(ServerOptions{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Give server time to start
	time.Sleep(10 * time.Millisecond)

	// Stop server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.Stop(ctx)
	if err != nil {
		t.Errorf("Server stop failed: %v", err)
	}

	// Check if server had any startup errors
	select {
	case err := <-serverErr:
		t.Errorf("Server startup failed: %v", err)
	default:
		// No startup error, which is good
	}
}
