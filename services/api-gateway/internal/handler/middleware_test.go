package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap/zaptest"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute)

	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}

	if rl.limiters == nil {
		t.Error("Limiters map not initialized")
	}

	if rl.burst < 1 {
		t.Errorf("Expected burst >= 1, got %d", rl.burst)
	}

	if rl.maxClients <= 0 {
		t.Errorf("Expected maxClients > 0, got %d", rl.maxClients)
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	// Use a very low rate for testing - 3 requests per minute
	rl := NewRateLimiter(3, time.Minute)
	defer rl.Close()

	clientID := "test-client"

	// The token bucket starts with burst capacity, so first requests may be allowed
	// Test basic functionality: allow some requests, then should be limited
	allowed := 0
	blocked := 0

	// Try multiple requests
	for i := 0; i < 10; i++ {
		if rl.Allow(clientID) {
			allowed++
		} else {
			blocked++
		}
	}

	// Should have some allowed and some blocked
	if allowed == 0 {
		t.Error("Expected some requests to be allowed")
	}
	if blocked == 0 {
		t.Error("Expected some requests to be blocked")
	}

	// Different client should be allowed initially
	if !rl.Allow("different-client") {
		t.Error("Different client should be allowed")
	}
}

func TestRateLimiter_AllowWithExpiry(t *testing.T) {
	// Test basic token bucket behavior - just verify it works
	rl := NewRateLimiter(10, time.Minute)
	defer rl.Close()

	clientID := "test-client"

	// Should always work for first request due to burst
	if !rl.Allow(clientID) {
		t.Error("First request should be allowed due to burst capacity")
	}

	// Different client should work independently
	if !rl.Allow("different-client") {
		t.Error("Different client should be allowed")
	}

	// Test that rate limiter eventually limits requests
	allowed := 0
	for i := 0; i < 20; i++ {
		if rl.Allow(clientID) {
			allowed++
		}
	}

	// Should have allowed some but not all due to rate limiting
	if allowed == 20 {
		t.Error("Rate limiter should have limited some requests")
	}
	if allowed == 0 {
		t.Error("Rate limiter should have allowed some requests")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	// Set a lower rate limit to ensure rate limiting occurs
	rl := NewRateLimiter(1, time.Minute) // 1 request per minute
	defer rl.Close()
	middleware := RateLimitMiddleware(rl, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("success")); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	})

	wrappedHandler := middleware(handler)

	// First request should pass (due to burst capacity)
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("First request should pass, got status %d", w.Code)
	}

	if w.Body.String() != "success" {
		t.Errorf("First request should return success, got %s", w.Body.String())
	}

	// Second request should be rate limited (burst is exhausted)
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w = httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Second request should be rate limited, got status %d", w.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse error response: %v", err)
	}

	if response["error"] != "Rate limit exceeded" {
		t.Errorf("Expected error message 'Rate limit exceeded', got '%s'", response["error"])
	}
}

func TestRateLimitMiddleware_XForwardedFor(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	defer rl.Close()
	middleware := RateLimitMiddleware(rl, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	// First request with X-Forwarded-For
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("First request should pass, got status %d", w.Code)
	}

	// Second request with same X-Forwarded-For should be blocked
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	w = httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Second request should be rate limited, got status %d", w.Code)
	}
}

func TestJWTMiddleware_MissingToken(t *testing.T) {
	logger := zaptest.NewLogger(t)
	middleware := JWTMiddleware("secret", logger, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	if response["error"] != "Missing Authorization header" {
		t.Errorf("Expected error message about missing header, got '%s'", response["error"])
	}
}

func TestJWTMiddleware_InvalidFormat(t *testing.T) {
	logger := zaptest.NewLogger(t)
	middleware := JWTMiddleware("secret", logger, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	req.Header.Set("Authorization", "InvalidFormat")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	if response["error"] != "Invalid Authorization header format" {
		t.Errorf("Expected error message about invalid format, got '%s'", response["error"])
	}
}

func TestJWTMiddleware_ValidToken(t *testing.T) {
	secret := "test-secret"
	logger := zaptest.NewLogger(t)
	middleware := JWTMiddleware(secret, logger, nil)

	// Create a valid token
	claims := &Claims{
		UserID: "user123",
		Email:  "test@example.com",
		Role:   "user",
	}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	// Test handler that checks headers
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-ID") != "user123" {
			t.Error("X-User-ID header not set correctly")
		}
		if r.Header.Get("X-User-Email") != "test@example.com" {
			t.Error("X-User-Email header not set correctly")
		}
		if r.Header.Get("X-User-Role") != "user" {
			t.Error("X-User-Role header not set correctly")
		}
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestJWTMiddleware_InvalidToken(t *testing.T) {
	logger := zaptest.NewLogger(t)
	middleware := JWTMiddleware("secret", logger, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	if response["error"] != "Invalid token" {
		t.Errorf("Expected error message about invalid token, got '%s'", response["error"])
	}
}

func TestJWTMiddleware_ExpiredToken(t *testing.T) {
	secret := "test-secret"
	logger := zaptest.NewLogger(t)
	middleware := JWTMiddleware(secret, logger, nil)

	// Create an expired token
	claims := &Claims{
		UserID: "user123",
		Email:  "test@example.com",
		Role:   "user",
	}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) // Expired

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestJWTMiddleware_HealthCheckBypass(t *testing.T) {
	logger := zaptest.NewLogger(t)
	middleware := JWTMiddleware("secret", logger, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("healthy")); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	})

	wrappedHandler := middleware(handler)

	// Test health check endpoint bypasses auth
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Health check should bypass auth, got status %d", w.Code)
	}

	if w.Body.String() != "healthy" {
		t.Errorf("Expected 'healthy', got '%s'", w.Body.String())
	}

	// Test metrics endpoint bypasses auth
	req = httptest.NewRequest("GET", "/metrics", nil)
	w = httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Metrics should bypass auth, got status %d", w.Code)
	}
}

func TestRateLimiter_Close(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute)

	// Test that Close doesn't panic
	rl.Close()

	// Test that we can call Close multiple times safely
	rl.Close()
}

func TestGetUserFromContext(t *testing.T) {
	// Test with context containing user claims
	claims := &Claims{
		UserID: "user123",
		Email:  "test@example.com",
		Role:   "admin",
	}

	ctx := context.WithValue(context.Background(), UserContextKey, claims)

	retrievedClaims, ok := GetUserFromContext(ctx)
	if !ok {
		t.Error("Expected to retrieve user from context")
	}

	if retrievedClaims.UserID != "user123" {
		t.Errorf("Expected UserID 'user123', got '%s'", retrievedClaims.UserID)
	}

	if retrievedClaims.Email != "test@example.com" {
		t.Errorf("Expected Email 'test@example.com', got '%s'", retrievedClaims.Email)
	}

	if retrievedClaims.Role != "admin" {
		t.Errorf("Expected Role 'admin', got '%s'", retrievedClaims.Role)
	}

	// Test with empty context
	emptyCtx := context.Background()
	_, ok = GetUserFromContext(emptyCtx)
	if ok {
		t.Error("Expected no user in empty context")
	}

	// Test with context containing wrong type
	wrongCtx := context.WithValue(context.Background(), UserContextKey, "wrong-type")
	_, ok = GetUserFromContext(wrongCtx)
	if ok {
		t.Error("Expected no user when context contains wrong type")
	}
}

func TestRateLimiter_CleanupStops(t *testing.T) {
	rl := NewRateLimiter(1, 50*time.Millisecond)

	// Allow one request to populate the map
	rl.Allow("test-client")

	// Close the rate limiter
	rl.Close()

	// Wait a bit to ensure cleanup would have run
	time.Sleep(100 * time.Millisecond)

	// The test passes if it doesn't hang, meaning cleanup goroutine stopped
}

func TestJWTMiddleware_UnsupportedSigningMethod(t *testing.T) {
	secret := "test-secret"
	logger := zaptest.NewLogger(t)
	middleware := JWTMiddleware(secret, logger, nil)

	// Create a token with RSA algorithm (unsupported)
	claims := &Claims{
		UserID: "user123",
		Email:  "test@example.com",
		Role:   "user",
	}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))

	// Create a malformed token with RSA256 header that will trigger the signing method check
	tokenString := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoidXNlcjEyMyIsImVtYWlsIjoidGVzdEBleGFtcGxlLmNvbSIsInJvbGUiOiJ1c2VyIiwiZXhwIjoxNjQwOTk1MjAwfQ.invalid-signature"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	if response["error"] != "Invalid token" {
		t.Errorf("Expected error message about invalid token, got '%s'", response["error"])
	}
}

func TestRateLimiter_MemoryLimit(t *testing.T) {
	// Test that rate limiter handles maxClients limit
	rl := NewRateLimiter(10, time.Minute)
	defer rl.Close()

	// Manually set a lower maxClients for testing
	rl.maxClients = 2

	// Add 3 clients (should trigger eviction)
	rl.Allow("client1")
	rl.Allow("client2")
	rl.Allow("client3") // This should evict the oldest

	// Verify that we don't exceed maxClients
	rl.mutex.RLock()
	clientCount := len(rl.limiters)
	rl.mutex.RUnlock()

	if clientCount > rl.maxClients {
		t.Errorf("Client count %d exceeds maxClients %d", clientCount, rl.maxClients)
	}
}

func TestRateLimitMiddleware_JSONResponse(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute) // Set limit to 1 request per minute
	defer rl.Close()

	middleware := RateLimitMiddleware(rl, nil)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should pass due to burst capacity
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("First request should pass, got status %d", w.Code)
	}

	// Second request should be rate limited
	req = httptest.NewRequest("GET", "/api/v1/test", nil)
	req.RemoteAddr = "192.168.1.1:12345" // Same IP
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status %d, got %d", http.StatusTooManyRequests, w.Code)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Expected Content-Type: application/json")
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	if response["error"] != "Rate limit exceeded" {
		t.Errorf("Expected 'Rate limit exceeded', got '%s'", response["error"])
	}
}

func TestJWTMiddleware_InvalidClaims(t *testing.T) {
	logger := zaptest.NewLogger(t)
	secret := "test-secret-key-for-testing"

	// Create a token with empty/invalid claims structure
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"invalid": "claims",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte(secret))

	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	middleware := JWTMiddleware(secret, logger, nil)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	if response["error"] != "Invalid token claims" {
		t.Errorf("Expected 'Invalid token claims', got '%s'", response["error"])
	}
}

func TestJWTMiddlewareWithMetrics_AllPaths(t *testing.T) {
	logger := zaptest.NewLogger(t)
	secret := "test-secret-key-for-testing"
	metrics := NewTestMetrics()

	testCases := []struct {
		name           string
		setupRequest   func() *http.Request
		expectedStatus int
	}{
		{
			name: "Missing header with metrics",
			setupRequest: func() *http.Request {
				return httptest.NewRequest("GET", "/api/v1/orders", nil)
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "Invalid format with metrics",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/api/v1/orders", nil)
				req.Header.Set("Authorization", "InvalidFormat")
				return req
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "Invalid token with metrics",
			setupRequest: func() *http.Request {
				req := httptest.NewRequest("GET", "/api/v1/orders", nil)
				req.Header.Set("Authorization", "Bearer invalid.token.here")
				return req
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "Valid token with metrics",
			setupRequest: func() *http.Request {
				claims := &Claims{
					UserID: "user123",
					Email:  "test@example.com",
					Role:   "user",
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
					},
				}
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte(secret))

				req := httptest.NewRequest("GET", "/api/v1/orders", nil)
				req.Header.Set("Authorization", "Bearer "+tokenString)
				return req
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "Invalid claims with metrics",
			setupRequest: func() *http.Request {
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
					"invalid": "claims",
					"exp":     time.Now().Add(time.Hour).Unix(),
				})
				tokenString, _ := token.SignedString([]byte(secret))

				req := httptest.NewRequest("GET", "/api/v1/orders", nil)
				req.Header.Set("Authorization", "Bearer "+tokenString)
				return req
			},
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.setupRequest()
			w := httptest.NewRecorder()

			middleware := JWTMiddleware(secret, logger, metrics)
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			handler.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
			}
		})
	}
}

func TestRateLimitMiddlewareWithMetrics_Paths(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	defer rl.Close()
	metrics := NewTestMetrics()

	// Test allowed request
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	w := httptest.NewRecorder()

	middleware := RateLimitMiddleware(rl, metrics)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("First request should be allowed, got status %d", w.Code)
	}

	// Test blocked request (rate limit exceeded)
	req = httptest.NewRequest("GET", "/api/v1/test", nil)
	w = httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Second request should be blocked, got status %d", w.Code)
	}
}
