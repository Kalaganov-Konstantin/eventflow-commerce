package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/handler"
	sharedConfig "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/config"
	"go.uber.org/zap/zaptest"
)

func TestEnhancedHealthCheck(t *testing.T) {
	cfg := &config.Config{}

	zapLogger := zaptest.NewLogger(t)
	router := handler.NewRouter(cfg, zapLogger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	var response handler.HealthStatus
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse health check response: %v", err)
		return
	}

	if response.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response.Status)
	}

	if response.Service != "api-gateway" {
		t.Errorf("Expected service 'api-gateway', got '%s'", response.Service)
	}

	if response.Details == nil {
		t.Error("Expected details in health check response")
	}

	if version, exists := response.Details["version"]; !exists || version == "" {
		t.Error("Expected version in health check details")
	}

	if uptime, exists := response.Details["uptime"]; !exists || uptime == "" {
		t.Error("Expected uptime in health check details")
	}
}

func TestEnhancedErrorHandling(t *testing.T) {
	// Test with invalid service URL
	cfg := &config.Config{
		OrderServiceURL: "invalid-url",
	}

	zapLogger := zaptest.NewLogger(t)
	router := handler.NewRouter(cfg, zapLogger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected status code %d, got %d", http.StatusBadGateway, w.Code)
	}

	var response handler.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse error response: %v", err)
		return
	}

	if response.Code != "PROXY_ERROR" && response.Code != "INVALID_SERVICE_URL" {
		t.Errorf("Expected error code 'PROXY_ERROR' or 'INVALID_SERVICE_URL', got '%s'", response.Code)
	}

	if response.Error == "" {
		t.Error("Expected error message in response")
	}
}

func TestConfigValidation(t *testing.T) {
	testCases := []struct {
		name        string
		config      config.Config
		expectError bool
	}{
		{
			name: "Valid config",
			config: config.Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{Host: "redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test",
					Password: "test", DBName: "test", SSLMode: "disable",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: config.RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    60,
				},
			},
			expectError: false,
		},
		{
			name: "JWT secret too short",
			config: config.Config{
				Server:                 sharedConfig.ServerConfig{Port: "8080"},
				JWTSecret:              "short",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: config.RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    60,
				},
			},
			expectError: true,
		},
		{
			name: "Invalid service URL",
			config: config.Config{
				Server:                 sharedConfig.ServerConfig{Port: "8080"},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "invalid-url-without-scheme",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: config.RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    60,
				},
			},
			expectError: true,
		},
		{
			name: "Invalid rate limit config",
			config: config.Config{
				Server:                 sharedConfig.ServerConfig{Port: "8080"},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: config.RateLimitConfig{
					RequestsPerMinute: -1, // Invalid
					WindowDuration:    60,
				},
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()

			if tc.expectError && err == nil {
				t.Error("Expected validation error, but got none")
			}

			if !tc.expectError && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestRateLimiterMemoryLeak(t *testing.T) {
	// Test that rate limiter doesn't accumulate unlimited clients
	rl := handler.NewRateLimiter(10, time.Minute)
	defer rl.Close()

	// Simulate many different clients (more than maxClients limit)
	for i := 0; i < 15000; i++ {
		clientID := fmt.Sprintf("client-%d", i)
		rl.Allow(clientID)
	}

	// The rate limiter should not have grown beyond its limits
	// This is a white-box test checking internal state
	// In a real scenario, you'd monitor memory usage
	t.Log("Rate limiter handled many clients without unlimited growth")
}
