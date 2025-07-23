package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/handler"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/server"
	sharedConfig "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap/zaptest"
)

func TestAPIGateway_Integration(t *testing.T) {
	// Create a mock backend service
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Echo back some request info for testing
		response := map[string]interface{}{
			"path":    r.URL.Path,
			"method":  r.Method,
			"headers": r.Header,
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer backend.Close()

	// Create test configuration
	cfg := &config.Config{
		Server: sharedConfig.ServerConfig{
			Host: "localhost",
			Port: "0", // Use random port
		},
		Service: sharedConfig.ServiceConfig{
			Name:    "test-api-gateway",
			Version: "1.0.0",
		},
		JWTSecret:              "test-secret-key-for-integration-test",
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

	// Use test metrics to avoid conflicts
	testMetrics := handler.NewTestMetrics()

	// Create server
	srv := server.NewServer(server.ServerOptions{
		Config:  cfg,
		Logger:  logger,
		Metrics: testMetrics,
	})

	// Test health check
	t.Run("HealthCheck", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		srv.GetHTTPServer().Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Errorf("Failed to parse health response: %v", err)
		}

		if response["status"] != "healthy" {
			t.Errorf("Expected healthy status, got %v", response["status"])
		}
	})

	// Test metrics endpoint
	t.Run("MetricsEndpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/metrics", nil)
		w := httptest.NewRecorder()

		srv.GetHTTPServer().Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		// Metrics should contain some prometheus format data
		body := w.Body.String()
		if len(body) == 0 {
			t.Error("Expected metrics data, got empty response")
		}
	})

	// Test rate limiting
	t.Run("RateLimiting", func(t *testing.T) {
		// Create a valid JWT token for testing
		claims := &handler.Claims{
			UserID: "test-user",
			Email:  "test@example.com",
			Role:   "user",
		}
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(cfg.JWTSecret))
		if err != nil {
			t.Fatalf("Failed to create test token: %v", err)
		}

		// Create a rate limiter with very low limits for testing
		testCfg := *cfg
		testCfg.RateLimit.RequestsPerMinute = 2

		testSrv := server.NewServer(server.ServerOptions{
			Config:  &testCfg,
			Logger:  logger,
			Metrics: handler.NewTestMetrics(),
		})

		// Make requests up to the limit
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
			req.Header.Set("Authorization", "Bearer "+tokenString)
			w := httptest.NewRecorder()

			testSrv.GetHTTPServer().Handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Request %d should succeed, got status %d", i+1, w.Code)
			}
		}

		// Next request should be rate limited
		req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()

		testSrv.GetHTTPServer().Handler.ServeHTTP(w, req)

		if w.Code != http.StatusTooManyRequests {
			t.Errorf("Expected rate limit error, got status %d", w.Code)
		}
	})

	// Test JWT authentication
	t.Run("JWTAuthentication", func(t *testing.T) {
		// Test without token
		req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
		w := httptest.NewRecorder()

		srv.GetHTTPServer().Handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected unauthorized, got status %d", w.Code)
		}

		// Test with invalid token
		req = httptest.NewRequest("GET", "/api/v1/orders/123", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		w = httptest.NewRecorder()

		srv.GetHTTPServer().Handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected unauthorized for invalid token, got status %d", w.Code)
		}

		// Test with valid token
		claims := &handler.Claims{
			UserID: "test-user",
			Email:  "test@example.com",
			Role:   "user",
		}
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(cfg.JWTSecret))
		if err != nil {
			t.Fatalf("Failed to create test token: %v", err)
		}

		req = httptest.NewRequest("GET", "/api/v1/orders/123", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w = httptest.NewRecorder()

		srv.GetHTTPServer().Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected success with valid token, got status %d", w.Code)
		}
	})

	// Test proxy functionality
	t.Run("ProxyRequests", func(t *testing.T) {
		// Create valid token
		claims := &handler.Claims{
			UserID: "test-user",
			Email:  "test@example.com",
			Role:   "user",
		}
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(cfg.JWTSecret))
		if err != nil {
			t.Fatalf("Failed to create test token: %v", err)
		}

		testCases := []struct {
			name                string
			path                string
			expectedBackendPath string
		}{
			{
				name:                "Order service",
				path:                "/api/v1/orders/123",
				expectedBackendPath: "/123",
			},
			{
				name:                "Payment service",
				path:                "/api/v1/payments/456",
				expectedBackendPath: "/456",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", tc.path, nil)
				req.Header.Set("Authorization", "Bearer "+tokenString)
				w := httptest.NewRecorder()

				srv.GetHTTPServer().Handler.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Errorf("Expected success, got status %d", w.Code)
				}

				var response map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Errorf("Failed to parse response: %v", err)
					return
				}

				if response["path"] != tc.expectedBackendPath {
					t.Errorf("Expected backend path %s, got %v", tc.expectedBackendPath, response["path"])
				}
			})
		}
	})

	// Test graceful shutdown
	t.Run("GracefulShutdown", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := srv.Stop(ctx)
		if err != nil {
			t.Errorf("Server shutdown failed: %v", err)
		}
	})
}
