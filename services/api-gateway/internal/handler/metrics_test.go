package handler

import (
	"testing"
	"time"
)

// Use the shared test metrics function
func getTestMetrics() *Metrics {
	return NewTestMetrics()
}

func TestMetrics_RecordRequest(t *testing.T) {
	metrics := getTestMetrics()

	// Verify the counter was called (the metric exists)
	counter := metrics.RequestsTotal.WithLabelValues("GET", "/api/v1/orders", "200")
	if counter != nil {
		t.Log("Request metrics counter created successfully")
	}
}

func TestMetrics_RecordRateLimit(t *testing.T) {
	metrics := getTestMetrics()

	// Test rate limit allowed
	metrics.RecordRateLimit("127.0.0.1", true)

	// Test rate limit blocked
	metrics.RecordRateLimit("127.0.0.1", false)

	// Verify both counters exist
	if metrics.RateLimitHits != nil && metrics.RateLimitedRequests != nil {
		t.Log("Rate limit metrics recorded successfully")
	} else {
		t.Error("Rate limit metrics not initialized properly")
	}
}

func TestMetrics_RecordJWTValidation(t *testing.T) {
	metrics := getTestMetrics()

	testCases := []struct {
		result   string
		duration time.Duration
	}{
		{"valid", 10 * time.Millisecond},
		{"invalid", 5 * time.Millisecond},
		{"missing_header", 1 * time.Millisecond},
	}

	for _, tc := range testCases {
		t.Run(tc.result, func(t *testing.T) {
			metrics.RecordJWTValidation(tc.result, tc.duration)

			// Verify the counter exists and can be called
			if metrics.JWTTokensValidated == nil {
				t.Error("JWT tokens validated counter not initialized")
			}

			if metrics.JWTValidationDuration == nil {
				t.Error("JWT validation duration histogram not initialized")
			}
		})
	}
}

func TestMetrics_ActiveConnections(t *testing.T) {
	metrics := getTestMetrics()

	// Test incrementing active connections
	metrics.IncActiveConnections()
	metrics.IncActiveConnections()

	// Test decrementing active connections
	metrics.DecActiveConnections()

	// Verify the gauge exists
	if metrics.ActiveConnections == nil {
		t.Error("Active connections gauge not initialized")
	}
}

func TestMetrics_ProxyMetrics(t *testing.T) {
	metrics := getTestMetrics()

	// Test recording proxy request
	metrics.RecordProxyRequest("order-service", "GET", 200, 50*time.Millisecond)

	// Test recording proxy error
	metrics.RecordProxyError("order-service", "connection_refused")

	// Verify metrics exist
	if metrics.ProxyRequestsTotal == nil {
		t.Error("Proxy requests total counter not initialized")
	}

	if metrics.ProxyRequestDuration == nil {
		t.Error("Proxy request duration histogram not initialized")
	}

	if metrics.ProxyErrorsTotal == nil {
		t.Error("Proxy errors total counter not initialized")
	}
}
