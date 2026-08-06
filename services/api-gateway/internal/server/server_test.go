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
	sharedmw "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/middleware"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap/zaptest"
)

var testMetrics = handler.NewMetrics()

func newTestServer(t *testing.T, opts Options) *Server {
	t.Helper()

	srv, err := NewServer(opts)
	if err != nil {
		t.Fatalf("NewServer returned unexpected error: %v", err)
	}
	return srv
}

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

	srv := newTestServer(t, Options{
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
	srv := newTestServer(t, Options{
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
	srv := newTestServer(t, Options{
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
	srv := newTestServer(t, Options{
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
	srv := newTestServer(t, Options{
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
	srv := newTestServer(t, Options{
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
	srv := newTestServer(t, Options{
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

func validJWT(t *testing.T, secret string) string {
	t.Helper()

	claims := &handler.Claims{
		UserID: "test-user",
		Email:  "test@example.com",
		Role:   "user",
	}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to sign test token: %v", err)
	}
	return tokenString
}

func TestServer_RequestIDIsGeneratedAndForwarded(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend-Saw-Request-ID", r.Header.Get("X-Request-ID"))
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "localhost",
			Port: "8080",
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        backend.URL,
		PaymentServiceURL:      backend.URL,
		InventoryServiceURL:    backend.URL,
		NotificationServiceURL: backend.URL,
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 100,
			WindowDuration:    60,
		},
		ProxyTimeout: 5,
	}

	logger := zaptest.NewLogger(t)
	srv := newTestServer(t, Options{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	req.Header.Set("Authorization", "Bearer "+validJWT(t, cfg.JWTSecret))
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	responseID := w.Header().Get("X-Request-ID")
	if responseID == "" {
		t.Error("Expected a generated X-Request-ID on the response")
	}

	backendSawID := w.Header().Get("X-Backend-Saw-Request-ID")
	if backendSawID == "" {
		t.Error("Expected the backend to receive an X-Request-ID header")
	}

	if backendSawID != responseID {
		t.Errorf("Expected backend request ID %q to match response request ID %q", backendSawID, responseID)
	}
}

func TestServer_RequestIDIsPreservedFromClient(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend-Saw-Request-ID", r.Header.Get("X-Request-ID"))
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "localhost",
			Port: "8080",
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        backend.URL,
		PaymentServiceURL:      backend.URL,
		InventoryServiceURL:    backend.URL,
		NotificationServiceURL: backend.URL,
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 100,
			WindowDuration:    60,
		},
		ProxyTimeout: 5,
	}

	logger := zaptest.NewLogger(t)
	srv := newTestServer(t, Options{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	req.Header.Set("Authorization", "Bearer "+validJWT(t, cfg.JWTSecret))
	req.Header.Set("X-Request-ID", "client-supplied-id")
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if got := w.Header().Get("X-Request-ID"); got != "client-supplied-id" {
		t.Errorf("Expected client-supplied X-Request-ID to be preserved, got %q", got)
	}

	if got := w.Header().Get("X-Backend-Saw-Request-ID"); got != "client-supplied-id" {
		t.Errorf("Expected backend to receive the client-supplied X-Request-ID, got %q", got)
	}
}

func TestServer_RecoveryMiddlewareCatchesPanics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	panicking := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	wrapped := sharedmw.Chain(sharedmw.Recovery(logger), sharedmw.RequestID)(panicking)

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d after panic recovery, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestNewServer_ReturnsErrorWhenRouterConstructionFails(t *testing.T) {
	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "localhost",
			Port: "8080",
		},
		JWTSecret:              "test-secret-key-for-jwt-validation-testing",
		OrderServiceURL:        "http://%zz", // unparsable, so building its reverse proxy fails
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
		RateLimit: config.RateLimitConfig{
			RequestsPerMinute: 100,
			WindowDuration:    60,
		},
	}

	logger := zaptest.NewLogger(t)

	srv, err := NewServer(Options{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	if err == nil {
		t.Fatal("Expected NewServer to return an error when the router cannot be constructed")
	}
	if srv != nil {
		t.Error("Expected a nil server when construction fails")
	}
}
