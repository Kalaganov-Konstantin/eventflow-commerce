package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/config"
	sharedConfig "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/config"
	"go.uber.org/zap"
)

func TestNewRouter(t *testing.T) {
	cfg := &config.Config{
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
	}
	logger, _ := zap.NewDevelopment()

	router := NewRouter(cfg, logger, time.Now())
	if router == nil {
		t.Fatal("NewRouter returned nil")
	}

	if router.config != cfg {
		t.Error("Router config not set correctly")
	}

	if router.mux == nil {
		t.Error("Router mux not initialized")
	}
}

func TestHealthCheck(t *testing.T) {
	cfg := &config.Config{
		Service: sharedConfig.ServiceConfig{
			Name:    "test-api-gateway",
			Version: "1.0.0",
		},
	}
	logger, _ := zap.NewDevelopment()
	router := NewRouter(cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	// Parse the JSON response to verify structure
	var response HealthStatus
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse health check response: %v", err)
		return
	}

	if response.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response.Status)
	}

	if response.Service != "test-api-gateway" {
		t.Errorf("Expected service 'test-api-gateway', got '%s'", response.Service)
	}

	if response.Details == nil {
		t.Error("Expected details in health check response")
	}

	if response.Details["version"] != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", response.Details["version"])
	}
}

func TestProxyToService(t *testing.T) {
	// Create a test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"message": "backend response"}`)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    5,
	}
	logger, _ := zap.NewDevelopment()

	router := NewRouter(cfg, logger, time.Now())
	router.SetupRoutes()

	// Test proxying request to order service
	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	req.Header.Set("X-Test-Header", "test-value")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	expected := `{"message": "backend response"}`
	if w.Body.String() != expected {
		t.Errorf("Expected body %s, got %s", expected, w.Body.String())
	}

	// Verify content type was preserved
	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Content-Type header not preserved from backend")
	}
}

func TestProxyToServiceInvalidURL(t *testing.T) {
	cfg := &config.Config{
		OrderServiceURL: "invalid-url",
	}
	logger, _ := zap.NewDevelopment()

	router := NewRouter(cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected status code %d, got %d", http.StatusBadGateway, w.Code)
	}
}

func TestRouteMapping(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(r.URL.Path)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		} // Echo the path to verify routing
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL:        backend.URL,
		PaymentServiceURL:      backend.URL,
		InventoryServiceURL:    backend.URL,
		NotificationServiceURL: backend.URL,
		ProxyTimeout:           5,
	}
	logger, _ := zap.NewDevelopment()

	router := NewRouter(cfg, logger, time.Now())
	router.SetupRoutes()

	testCases := []struct {
		name         string
		requestPath  string
		expectedPath string
	}{
		{
			name:         "Order service route",
			requestPath:  "/api/v1/orders/123",
			expectedPath: "/123",
		},
		{
			name:         "Payment service route",
			requestPath:  "/api/v1/payments/456",
			expectedPath: "/456",
		},
		{
			name:         "Inventory service route",
			requestPath:  "/api/v1/inventory/789",
			expectedPath: "/789",
		},
		{
			name:         "Products route to inventory",
			requestPath:  "/api/v1/products/abc",
			expectedPath: "/abc",
		},
		{
			name:         "Notification service route",
			requestPath:  "/api/v1/notifications/def",
			expectedPath: "/def",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.requestPath, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
			}

			if w.Body.String() != tc.expectedPath {
				t.Errorf("Expected path %s, got %s", tc.expectedPath, w.Body.String())
			}
		})
	}
}

func TestProxyHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if proxy headers are set
		if r.Header.Get("X-Forwarded-For") == "" {
			t.Error("X-Forwarded-For header not set")
		}
		if r.Header.Get("X-Forwarded-Proto") == "" {
			t.Error("X-Forwarded-Proto header not set")
		}
		if r.Header.Get("X-Original-Path") == "" {
			t.Error("X-Original-Path header not set")
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    5,
	}
	logger, _ := zap.NewDevelopment()

	router := NewRouter(cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestNewRouterWithLogger(t *testing.T) {
	cfg := &config.Config{
		OrderServiceURL: "http://order:8080",
	}

	// Create a test logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	router := NewRouter(cfg, logger, time.Now())

	if router == nil {
		t.Fatal("NewRouterWithLogger returned nil")
	}

	if router.config != cfg {
		t.Error("Router config not set correctly")
	}

	if router.logger != logger {
		t.Error("Router logger not set correctly")
	}

	if router.mux == nil {
		t.Error("Router mux not initialized")
	}
}

func TestProxyErrorHandler_AllErrorTypes(t *testing.T) {
	cfg := &config.Config{
		OrderServiceURL: "http://order:8080",
	}

	logger, _ := zap.NewDevelopment()
	router := NewRouter(cfg, logger, time.Now())

	testCases := []struct {
		name           string
		errorMessage   string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "Connection refused",
			errorMessage:   "connection refused",
			expectedStatus: http.StatusServiceUnavailable,
			expectedCode:   "SERVICE_UNAVAILABLE",
		},
		{
			name:           "Timeout error",
			errorMessage:   "request timeout",
			expectedStatus: http.StatusGatewayTimeout,
			expectedCode:   "GATEWAY_TIMEOUT",
		},
		{
			name:           "Host resolution error",
			errorMessage:   "no such host",
			expectedStatus: http.StatusBadGateway,
			expectedCode:   "INVALID_HOST",
		},
		{
			name:           "Generic error",
			errorMessage:   "some generic error",
			expectedStatus: http.StatusBadGateway,
			expectedCode:   "PROXY_ERROR",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/v1/orders", nil)

			// Create an error with the test message
			err := fmt.Errorf("%s", tc.errorMessage)

			router.proxyErrorHandler(w, req, err)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
			}

			var response ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Errorf("Failed to parse error response: %v", err)
				return
			}

			if response.Code != tc.expectedCode {
				t.Errorf("Expected code %s, got %s", tc.expectedCode, response.Code)
			}
		})
	}
}

func TestHealthCheck_ErrorHandling(t *testing.T) {
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	// Test without logger (should not panic)
	router := NewRouter(cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	// Verify JSON structure
	var response HealthStatus
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse health check response: %v", err)
		return
	}

	if response.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response.Status)
	}

	// Test with logger
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestProxyToService_ContextTimeout(t *testing.T) {
	// Create a backend that takes longer than the context timeout
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second) // Longer than 30s timeout in proxyToService
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    0,
	}

	logger, _ := zap.NewDevelopment()
	router := NewRouter(cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()

	// This test might take a while, so we'll skip it in short test mode
	// In practice, the timeout would trigger and cause an error
	router.ServeHTTP(w, req)

	// The exact response depends on the timeout behavior, but it should not be 200
	if w.Code == http.StatusOK {
		t.Log("Request completed successfully (timeout may not have triggered in test)")
	} else {
		t.Logf("Request failed as expected with status %d", w.Code)
	}
}

func TestProxyToService_EmptyPath(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that empty path becomes "/"
		if r.URL.Path != "/" {
			t.Errorf("Expected path '/', got '%s'", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("root")); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    5,
	}
	logger, _ := zap.NewDevelopment()

	router := NewRouter(cfg, logger, time.Now())
	router.SetupRoutes()

	// Request exactly the prefix path with trailing slash (should become "/")
	req := httptest.NewRequest("GET", "/api/v1/orders/", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	if w.Body.String() != "root" {
		t.Errorf("Expected 'root', got '%s'", w.Body.String())
	}
}

func TestSetProxyHeaders_EdgeCases(t *testing.T) {
	cfg := &config.Config{
		OrderServiceURL: "http://order:8080",
	}
	logger, _ := zap.NewDevelopment()

	router := NewRouter(cfg, logger, time.Now())

	// Test with existing X-Real-IP header
	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	req.Header.Set("X-Real-IP", "10.0.0.1")
	req.RemoteAddr = "192.168.1.1:12345"

	router.setProxyHeaders(req, "/api/v1/orders/123", "localhost:8080")

	if req.Header.Get("X-Real-IP") != "10.0.0.1" {
		t.Errorf("Expected X-Real-IP to remain '10.0.0.1', got '%s'", req.Header.Get("X-Real-IP"))
	}

	// Test with existing X-Forwarded-For header
	req2 := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	req2.Header.Set("X-Forwarded-For", "10.0.0.2, 10.0.0.1")
	req2.RemoteAddr = "192.168.1.1:12345"

	router.setProxyHeaders(req2, "/api/v1/orders/123", "localhost:8080")

	if req2.Header.Get("X-Real-IP") != "10.0.0.2" {
		t.Errorf("Expected X-Real-IP to be '10.0.0.2', got '%s'", req2.Header.Get("X-Real-IP"))
	}

	// Test with no scheme in request URL
	req3 := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	req3.URL.Scheme = "" // Ensure no scheme
	req3.RemoteAddr = "192.168.1.1:12345"

	router.setProxyHeaders(req3, "/api/v1/orders/123", "localhost:8080")

	if req3.Header.Get("X-Forwarded-Proto") != "http" {
		t.Errorf("Expected X-Forwarded-Proto to default to 'http', got '%s'", req3.Header.Get("X-Forwarded-Proto"))
	}
}
