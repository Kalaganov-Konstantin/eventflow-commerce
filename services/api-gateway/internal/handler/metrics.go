package handler

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all the Prometheus metrics for the API Gateway
type Metrics struct {
	// HTTP request metrics
	RequestsTotal     *prometheus.CounterVec
	RequestDuration   *prometheus.HistogramVec
	ActiveConnections prometheus.Gauge

	// Rate limiting metrics
	RateLimitHits       *prometheus.CounterVec
	RateLimitedRequests *prometheus.CounterVec

	// Proxy metrics
	ProxyRequestsTotal   *prometheus.CounterVec
	ProxyRequestDuration *prometheus.HistogramVec
	ProxyErrorsTotal     *prometheus.CounterVec

	// JWT metrics
	JWTTokensValidated    *prometheus.CounterVec
	JWTValidationDuration prometheus.Histogram
}

// NewMetrics creates and registers all metrics
func NewMetrics() *Metrics {
	return &Metrics{
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "api_gateway_requests_total",
				Help: "Total number of HTTP requests processed by the API gateway",
			},
			[]string{"method", "path", "status_code"},
		),
		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "api_gateway_request_duration_seconds",
				Help:    "Duration of HTTP requests processed by the API gateway",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path", "status_code"},
		),
		ActiveConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "api_gateway_active_connections",
				Help: "Number of active connections to the API gateway",
			},
		),
		RateLimitHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "api_gateway_rate_limit_hits_total",
				Help: "Total number of rate limit checks",
			},
			[]string{"client_ip", "allowed"},
		),
		RateLimitedRequests: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "api_gateway_rate_limited_requests_total",
				Help: "Total number of rate limited requests",
			},
			[]string{"client_ip"},
		),
		ProxyRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "api_gateway_proxy_requests_total",
				Help: "Total number of proxied requests",
			},
			[]string{"target_service", "method", "status_code"},
		),
		ProxyRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "api_gateway_proxy_request_duration_seconds",
				Help:    "Duration of proxied requests",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"target_service", "method", "status_code"},
		),
		ProxyErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "api_gateway_proxy_errors_total",
				Help: "Total number of proxy errors",
			},
			[]string{"target_service", "error_type"},
		),
		JWTTokensValidated: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "api_gateway_jwt_tokens_validated_total",
				Help: "Total number of JWT tokens validated",
			},
			[]string{"result"},
		),
		JWTValidationDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "api_gateway_jwt_validation_duration_seconds",
				Help:    "Duration of JWT token validation",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
		),
	}
}

// RecordRequest records an HTTP request
func (m *Metrics) RecordRequest(method, path string, statusCode int, duration time.Duration) {
	statusStr := strconv.Itoa(statusCode)
	m.RequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	m.RequestDuration.WithLabelValues(method, path, statusStr).Observe(duration.Seconds())
}

// RecordRateLimit records a rate limit check
func (m *Metrics) RecordRateLimit(clientIP string, allowed bool) {
	allowedStr := strconv.FormatBool(allowed)
	m.RateLimitHits.WithLabelValues(clientIP, allowedStr).Inc()

	if !allowed {
		m.RateLimitedRequests.WithLabelValues(clientIP).Inc()
	}
}

// RecordProxyRequest records a proxied request
func (m *Metrics) RecordProxyRequest(targetService, method string, statusCode int, duration time.Duration) {
	statusStr := strconv.Itoa(statusCode)
	m.ProxyRequestsTotal.WithLabelValues(targetService, method, statusStr).Inc()
	m.ProxyRequestDuration.WithLabelValues(targetService, method, statusStr).Observe(duration.Seconds())
}

// RecordProxyError records a proxy error
func (m *Metrics) RecordProxyError(targetService, errorType string) {
	m.ProxyErrorsTotal.WithLabelValues(targetService, errorType).Inc()
}

// RecordJWTValidation records JWT token validation
func (m *Metrics) RecordJWTValidation(result string, duration time.Duration) {
	m.JWTTokensValidated.WithLabelValues(result).Inc()
	m.JWTValidationDuration.Observe(duration.Seconds())
}

// IncActiveConnections increments active connections counter
func (m *Metrics) IncActiveConnections() {
	m.ActiveConnections.Inc()
}

// DecActiveConnections decrements active connections counter
func (m *Metrics) DecActiveConnections() {
	m.ActiveConnections.Dec()
}

// Test singleton for preventing duplicate metric registration in tests
var (
	testMetrics     *Metrics
	testMetricsOnce sync.Once
	testRegistry    *prometheus.Registry
)

// NewTestMetrics creates a singleton metrics instance for tests with separate registry
func NewTestMetrics() *Metrics {
	testMetricsOnce.Do(func() {
		// Create a separate registry for tests to avoid global conflicts
		testRegistry = prometheus.NewRegistry()

		testMetrics = &Metrics{
			RequestsTotal: prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "test_api_gateway_requests_total",
					Help: "Total number of HTTP requests processed by the API gateway (test)",
				},
				[]string{"method", "path", "status_code"},
			),
			RequestDuration: prometheus.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "test_api_gateway_request_duration_seconds",
					Help:    "Duration of HTTP requests processed by the API gateway (test)",
					Buckets: prometheus.DefBuckets,
				},
				[]string{"method", "path", "status_code"},
			),
			ActiveConnections: prometheus.NewGauge(
				prometheus.GaugeOpts{
					Name: "test_api_gateway_active_connections",
					Help: "Number of active connections to the API gateway (test)",
				},
			),
			RateLimitHits: prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "test_api_gateway_rate_limit_hits_total",
					Help: "Total number of rate limit checks (test)",
				},
				[]string{"client_ip", "allowed"},
			),
			RateLimitedRequests: prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "test_api_gateway_rate_limited_requests_total",
					Help: "Total number of rate limited requests (test)",
				},
				[]string{"client_ip"},
			),
			ProxyRequestsTotal: prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "test_api_gateway_proxy_requests_total",
					Help: "Total number of proxied requests (test)",
				},
				[]string{"target_service", "method", "status_code"},
			),
			ProxyRequestDuration: prometheus.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "test_api_gateway_proxy_request_duration_seconds",
					Help:    "Duration of proxied requests (test)",
					Buckets: prometheus.DefBuckets,
				},
				[]string{"target_service", "method", "status_code"},
			),
			ProxyErrorsTotal: prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "test_api_gateway_proxy_errors_total",
					Help: "Total number of proxy errors (test)",
				},
				[]string{"target_service", "error_type"},
			),
			JWTTokensValidated: prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "test_api_gateway_jwt_tokens_validated_total",
					Help: "Total number of JWT tokens validated (test)",
				},
				[]string{"result"},
			),
			JWTValidationDuration: prometheus.NewHistogram(
				prometheus.HistogramOpts{
					Name:    "test_api_gateway_jwt_validation_duration_seconds",
					Help:    "Duration of JWT token validation (test)",
					Buckets: prometheus.DefBuckets,
				},
			),
		}

		// Register all metrics with test registry
		testRegistry.MustRegister(
			testMetrics.RequestsTotal,
			testMetrics.RequestDuration,
			testMetrics.ActiveConnections,
			testMetrics.RateLimitHits,
			testMetrics.RateLimitedRequests,
			testMetrics.ProxyRequestsTotal,
			testMetrics.ProxyRequestDuration,
			testMetrics.ProxyErrorsTotal,
			testMetrics.JWTTokensValidated,
			testMetrics.JWTValidationDuration,
		)
	})
	return testMetrics
}
