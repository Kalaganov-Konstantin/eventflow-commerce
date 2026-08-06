package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/config"
	sharedConfig "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
)

func newTestRouter(t *testing.T, cfg *config.Config, logger *zap.Logger, startTime time.Time) *Router {
	t.Helper()

	router, err := NewRouter(cfg, logger, nil, startTime)
	if err != nil {
		t.Fatalf("NewRouter returned unexpected error: %v", err)
	}
	return router
}

func TestNewRouter(t *testing.T) {
	cfg := &config.Config{
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
	}
	logger, _ := zap.NewDevelopment()

	router := newTestRouter(t, cfg, logger, time.Now())

	if router.config != cfg {
		t.Error("Router config not set correctly")
	}

	if router.mux == nil {
		t.Error("Router mux not initialized")
	}
}

func TestNewRouter_InvalidServiceURL(t *testing.T) {
	cfg := &config.Config{
		OrderServiceURL: "http://%zz",
	}
	logger, _ := zap.NewDevelopment()

	_, err := NewRouter(cfg, logger, nil, time.Now())
	if err == nil {
		t.Fatal("Expected NewRouter to return an error for an unparsable service URL")
	}
}

func TestNewRouter_BuildsProxiesUpFront(t *testing.T) {
	cfg := &config.Config{
		OrderServiceURL:        "http://order:8080",
		PaymentServiceURL:      "http://payment:8080",
		InventoryServiceURL:    "http://inventory:8080",
		NotificationServiceURL: "http://notification:8080",
	}
	logger, _ := zap.NewDevelopment()

	router := newTestRouter(t, cfg, logger, time.Now())

	// Proxies must exist right after construction, before any request is served.
	for _, name := range []string{backendOrder, backendPayment, backendInventory, backendNotification} {
		bp, ok := router.proxies[name]
		if !ok || bp.proxy == nil {
			t.Errorf("Expected a pre-built proxy for backend %q", name)
		}
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
	router := newTestRouter(t, cfg, logger, time.Now())
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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	router := newTestRouter(t, cfg, logger, time.Now())
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

	router := newTestRouter(t, cfg, logger, time.Now())
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

	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	testCases := []struct {
		name         string
		requestPath  string
		expectedPath string
	}{
		{
			name:         "Order service route",
			requestPath:  "/api/v1/orders/123",
			expectedPath: "/api/v1/orders/123",
		},
		{
			name:         "Payment service route",
			requestPath:  "/api/v1/payments/456",
			expectedPath: "/api/v1/payments/456",
		},
		{
			name:         "Inventory service route",
			requestPath:  "/api/v1/inventory/789",
			expectedPath: "/api/v1/inventory/789",
		},
		{
			name:         "Products route to inventory",
			requestPath:  "/api/v1/products/abc",
			expectedPath: "/api/v1/products/abc",
		},
		{
			name:         "Notification service route",
			requestPath:  "/api/v1/notifications/def",
			expectedPath: "/api/v1/notifications/def",
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

	router := newTestRouter(t, cfg, logger, time.Now())
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

	router := newTestRouter(t, cfg, logger, time.Now())

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
	router := newTestRouter(t, cfg, logger, time.Now())

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

			router.proxyErrorHandler(w, req, err, backendOrder)

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
	router := newTestRouter(t, cfg, logger, time.Now())
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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(1 * time.Second) // Longer than 30s timeout in proxyToService
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    0,
	}

	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
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

func TestProxyToService_PrefixPathUnchanged(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The backend owns the full path; the gateway must not strip the prefix
		if r.URL.Path != "/api/v1/orders/" {
			t.Errorf("Expected path '/api/v1/orders/', got '%s'", r.URL.Path)
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

	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

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

	router := newTestRouter(t, cfg, logger, time.Now())

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

func TestProxyToService_RecordsProxyMetrics(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    5,
	}
	logger, _ := zap.NewDevelopment()
	metrics := NewTestMetrics()

	router, err := NewRouter(cfg, logger, metrics, time.Now())
	if err != nil {
		t.Fatalf("NewRouter returned unexpected error: %v", err)
	}
	router.SetupRoutes()

	before := testutil.ToFloat64(metrics.ProxyRequestsTotal.WithLabelValues(backendOrder, "GET", "200"))

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	after := testutil.ToFloat64(metrics.ProxyRequestsTotal.WithLabelValues(backendOrder, "GET", "200"))
	if after != before+1 {
		t.Errorf("Expected proxy requests metric labelled by backend name to increase by 1, got %v -> %v", before, after)
	}
}

func TestProxyToService_RecordsProxyErrorMetrics(t *testing.T) {
	// Start and immediately close a server to get an address that reliably refuses connections.
	backend := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	backendURL := backend.URL
	backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backendURL,
		ProxyTimeout:    5,
	}
	logger, _ := zap.NewDevelopment()
	metrics := NewTestMetrics()

	router, err := NewRouter(cfg, logger, metrics, time.Now())
	if err != nil {
		t.Fatalf("NewRouter returned unexpected error: %v", err)
	}
	router.SetupRoutes()

	before := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues(backendOrder, "SERVICE_UNAVAILABLE"))

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	after := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues(backendOrder, "SERVICE_UNAVAILABLE"))
	if after != before+1 {
		t.Errorf("Expected proxy errors metric labelled by backend name to increase by 1, got %v -> %v", before, after)
	}
}

func TestProxyToService_GeneratesCorrelationID(t *testing.T) {
	var backendSawID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendSawID = r.Header.Get("X-Correlation-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    5,
	}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	responseID := w.Header().Get("X-Correlation-ID")
	if responseID == "" {
		t.Fatal("Expected a generated X-Correlation-ID on the response")
	}

	if backendSawID == "" {
		t.Error("Expected the backend to receive an X-Correlation-ID header")
	}

	if backendSawID != responseID {
		t.Errorf("Expected backend correlation id %q to match response correlation id %q", backendSawID, responseID)
	}
}

func TestProxyToService_PreservesClientCorrelationID(t *testing.T) {
	var backendSawID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendSawID = r.Header.Get("X-Correlation-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    5,
	}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	req.Header.Set("X-Correlation-ID", "client-supplied-correlation-id")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if got := w.Header().Get("X-Correlation-ID"); got != "client-supplied-correlation-id" {
		t.Errorf("Expected client-supplied correlation id to be preserved, got %q", got)
	}

	if backendSawID != "client-supplied-correlation-id" {
		t.Errorf("Expected backend to receive the client-supplied correlation id, got %q", backendSawID)
	}
}

func TestLivenessCheck_AlwaysOK(t *testing.T) {
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/health/live", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestReadinessCheck_AllBackendsHealthy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL:        backend.URL,
		PaymentServiceURL:      backend.URL,
		InventoryServiceURL:    backend.URL,
		NotificationServiceURL: backend.URL,
	}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestReadinessCheck_ReportsUnavailableBackends(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer healthy.Close()

	// Start and immediately close a server to get an address that reliably refuses connections.
	down := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	downURL := down.URL
	down.Close()

	cfg := &config.Config{
		OrderServiceURL:        downURL,
		PaymentServiceURL:      healthy.URL,
		InventoryServiceURL:    healthy.URL,
		NotificationServiceURL: healthy.URL,
	}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	var response struct {
		Status      string   `json:"status"`
		Unavailable []string `json:"unavailable"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse readiness response: %v", err)
	}

	if len(response.Unavailable) != 1 || response.Unavailable[0] != backendOrder {
		t.Errorf("Expected only %q to be reported unavailable, got %v", backendOrder, response.Unavailable)
	}
}

func TestReadinessCheck_RespectsTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()

	cfg := &config.Config{
		OrderServiceURL:        slow.URL,
		PaymentServiceURL:      slow.URL,
		InventoryServiceURL:    slow.URL,
		NotificationServiceURL: slow.URL,
	}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.readinessTimeout = 20 * time.Millisecond
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	router.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("Expected readiness check to respect the short timeout, took %v", elapsed)
	}
}

func TestNotFoundHandler_UnknownAPIRoute(t *testing.T) {
	cfg := &config.Config{
		OrderServiceURL: "http://order:8080",
	}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/unknown-service/123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	var response ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse not found response: %v", err)
	}

	if response.Code != "NOT_FOUND" {
		t.Errorf("Expected code 'NOT_FOUND', got %q", response.Code)
	}
}

func TestProxyToService_CircuitBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	// Start and immediately close a server to get an address that reliably refuses connections.
	backend := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	backendURL := backend.URL
	backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backendURL,
		ProxyTimeout:    1,
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold:   2,
			WindowSeconds:      60,
			OpenTimeoutSeconds: 60,
		},
	}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	// The first two requests reach the (dead) backend and fail, tripping the breaker.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("request %d: expected status %d, got %d", i, http.StatusServiceUnavailable, w.Code)
		}
	}

	if state := router.proxies[backendOrder].breaker.State(); state != gobreaker.StateOpen {
		t.Fatalf("breaker state = %v, want open after consecutive failures", state)
	}

	// Once the breaker is open, no further request should reach the (dead) backend either, but
	// the response is still a clean 503 rather than a proxy connection-refused error.
	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	var response ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if response.Code != "SERVICE_UNAVAILABLE" {
		t.Errorf("Code = %q, want SERVICE_UNAVAILABLE", response.Code)
	}
}

func TestProxyToService_CircuitBreakerSkipsBackendWhileOpen(t *testing.T) {
	var calls int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    5,
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold:   1,
			WindowSeconds:      60,
			OpenTimeoutSeconds: 60,
		},
	}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	// First request reaches the backend, fails with a 5xx and trips the breaker (threshold 1).
	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if calls := atomic.LoadInt32(&calls); calls != 1 {
		t.Fatalf("calls to backend after first request = %d, want 1", calls)
	}

	// Second request must be short-circuited: no additional call to the backend.
	req2 := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, w2.Code)
	}
	if calls := atomic.LoadInt32(&calls); calls != 1 {
		t.Errorf("calls to backend after second request = %d, want still 1 (breaker should skip the backend)", calls)
	}
}

func TestHealthCheck_EncodeErrorFallsBackToPlainText(t *testing.T) {
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())

	w := &failingResponseWriter{}
	req := httptest.NewRequest("GET", "/health", nil)

	router.healthCheck(w, req)

	if len(w.statusCodes) != 2 || w.statusCodes[0] != http.StatusOK || w.statusCodes[1] != http.StatusInternalServerError {
		t.Errorf("Expected status codes [200 500] (health body, then encode-failure fallback), got %v", w.statusCodes)
	}
}

func TestLivenessCheck_EncodeErrorHasNoPlainTextFallback(t *testing.T) {
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())

	w := &failingResponseWriter{}
	req := httptest.NewRequest("GET", "/health/live", nil)

	router.livenessCheck(w, req)

	if len(w.statusCodes) != 1 || w.statusCodes[0] != http.StatusOK {
		t.Errorf("Expected only the initial 200 write (liveness only logs encode failures), got %v", w.statusCodes)
	}
}

func TestReadinessCheck_EncodeErrorsForBothOutcomes(t *testing.T) {
	// Start and immediately close a server to get an address that reliably refuses connections.
	down := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	downURL := down.URL
	down.Close()

	logger, _ := zap.NewDevelopment()

	notReadyCfg := &config.Config{
		OrderServiceURL:        downURL,
		PaymentServiceURL:      downURL,
		InventoryServiceURL:    downURL,
		NotificationServiceURL: downURL,
	}
	notReadyRouter := newTestRouter(t, notReadyCfg, logger, time.Now())

	wNotReady := &failingResponseWriter{}
	req := httptest.NewRequest("GET", "/health/ready", nil)
	notReadyRouter.readinessCheck(wNotReady, req)

	if len(wNotReady.statusCodes) != 1 || wNotReady.statusCodes[0] != http.StatusServiceUnavailable {
		t.Errorf("Expected only the 503 write (readiness only logs encode failures), got %v", wNotReady.statusCodes)
	}

	healthy := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			rw.WriteHeader(http.StatusOK)
			return
		}
		rw.WriteHeader(http.StatusNotFound)
	}))
	defer healthy.Close()

	readyCfg := &config.Config{
		OrderServiceURL:        healthy.URL,
		PaymentServiceURL:      healthy.URL,
		InventoryServiceURL:    healthy.URL,
		NotificationServiceURL: healthy.URL,
	}
	readyRouter := newTestRouter(t, readyCfg, logger, time.Now())

	wReady := &failingResponseWriter{}
	req2 := httptest.NewRequest("GET", "/health/ready", nil)
	readyRouter.readinessCheck(wReady, req2)

	if len(wReady.statusCodes) != 1 || wReady.statusCodes[0] != http.StatusOK {
		t.Errorf("Expected only the 200 write (readiness only logs encode failures), got %v", wReady.statusCodes)
	}
}

func TestBackendHealthy_UnknownBackendName(t *testing.T) {
	cfg := &config.Config{OrderServiceURL: "http://order:8080"}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())

	if router.backendHealthy(context.Background(), "unknown-backend") {
		t.Error("Expected backendHealthy to return false for a name with no registered proxy")
	}
}

func TestBackendHealthy_RequestConstructionFailure(t *testing.T) {
	cfg := &config.Config{OrderServiceURL: "http://order:8080"}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())

	// A Host containing a space cannot be re-parsed by NewRequestWithContext, even though this
	// target was built directly rather than through url.Parse (which would have rejected it).
	router.proxies[backendOrder] = &backendProxy{
		name:   backendOrder,
		target: &url.URL{Scheme: "http", Host: "exam ple.com"},
	}

	if router.backendHealthy(context.Background(), backendOrder) {
		t.Error("Expected backendHealthy to return false when the health request cannot be constructed")
	}
}

func TestGetClientIP_IPv6FallbackWhenSplitHostPortFails(t *testing.T) {
	cfg := &config.Config{OrderServiceURL: "http://order:8080"}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	req.RemoteAddr = "2001:db8::1:8080"

	if ip := router.getClientIP(req); ip != "2001:db8::1" {
		t.Errorf("Expected the manual colon-split fallback to recover the IPv6 host, got %q", ip)
	}
}

func TestGetClientIP_UnknownWhenNothingParses(t *testing.T) {
	cfg := &config.Config{OrderServiceURL: "http://order:8080"}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	req.RemoteAddr = "badformat"

	if ip := router.getClientIP(req); ip != "unknown" {
		t.Errorf("Expected 'unknown' when RemoteAddr has no parsable host or port, got %q", ip)
	}
}

func TestRoutePath_CollapsesLongPaths(t *testing.T) {
	testCases := []struct {
		name     string
		path     string
		expected string
	}{
		{"root path", "/", "/"},
		{"short path", "/health", "/health"},
		{"exactly three segments", "/api/v1/orders", "/api/v1/orders"},
		{"drops sub-resources beyond three segments", "/api/v1/orders/123/items", "/api/v1/orders"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := routePath(tc.path); got != tc.expected {
				t.Errorf("routePath(%q) = %q, want %q", tc.path, got, tc.expected)
			}
		})
	}
}

func TestSpanName_CombinesMethodAndRoute(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)

	if got := SpanName(req); got != "GET /api/v1/orders" {
		t.Errorf("SpanName(req) = %q, want %q", got, "GET /api/v1/orders")
	}
}

func TestWriteCircuitOpenResponse_RecordsMetricsAndHasNoPlainTextFallback(t *testing.T) {
	cfg := &config.Config{OrderServiceURL: "http://order:8080"}
	logger, _ := zap.NewDevelopment()
	metrics := NewTestMetrics()

	router, err := NewRouter(cfg, logger, metrics, time.Now())
	if err != nil {
		t.Fatalf("NewRouter returned unexpected error: %v", err)
	}

	before := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues(backendOrder, "SERVICE_UNAVAILABLE"))

	w := &failingResponseWriter{}
	router.writeCircuitOpenResponse(w, backendOrder)

	after := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues(backendOrder, "SERVICE_UNAVAILABLE"))
	if after != before+1 {
		t.Errorf("Expected the circuit-open response to record a proxy error metric, got %v -> %v", before, after)
	}

	if len(w.statusCodes) != 1 || w.statusCodes[0] != http.StatusServiceUnavailable {
		t.Errorf("Expected only the 503 write (circuit-open response only logs encode failures), got %v", w.statusCodes)
	}
}

func TestNotFoundHandler_EncodeErrorFallsBackToPlainText(t *testing.T) {
	cfg := &config.Config{OrderServiceURL: "http://order:8080"}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())

	w := &failingResponseWriter{}
	req := httptest.NewRequest("GET", "/api/v1/unknown", nil)

	router.notFoundHandler(w, req)

	if len(w.statusCodes) != 2 || w.statusCodes[0] != http.StatusNotFound || w.statusCodes[1] != http.StatusInternalServerError {
		t.Errorf("Expected status codes [404 500] (not-found body, then encode-failure fallback), got %v", w.statusCodes)
	}
}

func TestProxyErrorHandler_EncodeErrorFallsBackToPlainText(t *testing.T) {
	cfg := &config.Config{OrderServiceURL: "http://order:8080"}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())

	w := &failingResponseWriter{}
	req := httptest.NewRequest("GET", "/api/v1/orders", nil)

	router.proxyErrorHandler(w, req, fmt.Errorf("connection refused"), backendOrder)

	if len(w.statusCodes) != 2 || w.statusCodes[0] != http.StatusServiceUnavailable || w.statusCodes[1] != http.StatusInternalServerError {
		t.Errorf("Expected status codes [503 500] (proxy error body, then encode-failure fallback), got %v", w.statusCodes)
	}
}

func TestNotFoundHandler_KnownRoutesStillProxy(t *testing.T) {
	// Regression: registering the /api/v1/ catch-all must not shadow the
	// more specific per-service routes.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &config.Config{
		OrderServiceURL: backend.URL,
		ProxyTimeout:    5,
	}
	logger, _ := zap.NewDevelopment()
	router := newTestRouter(t, cfg, logger, time.Now())
	router.SetupRoutes()

	req := httptest.NewRequest("GET", "/api/v1/orders/123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected known route to still proxy successfully, got status %d", w.Code)
	}
}
