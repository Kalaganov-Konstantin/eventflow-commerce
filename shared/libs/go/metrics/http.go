// Package metrics provides shared Prometheus instrumentation for service HTTP servers.
package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// HTTPMetrics records request counts and durations for a service's HTTP server.
type HTTPMetrics struct {
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

// NewHTTPMetrics registers <service>_http_requests_total and
// <service>_http_request_duration_seconds on registerer.
func NewHTTPMetrics(registerer prometheus.Registerer, service string) *HTTPMetrics {
	labels := []string{"method", "route", "status_code"}

	m := &HTTPMetrics{
		requestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: service + "_http_requests_total",
				Help: "Total number of HTTP requests processed.",
			},
			labels,
		),
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    service + "_http_request_duration_seconds",
				Help:    "Duration of HTTP requests in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			labels,
		),
	}

	registerer.MustRegister(m.requestsTotal, m.requestDuration)
	return m
}

// Middleware wraps mux, recording request count and duration labeled by method,
// matched route pattern and status code.
func (m *HTTPMetrics) Middleware(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		_, pattern := mux.Handler(r)

		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		mux.ServeHTTP(rec, r)

		route := routeLabel(pattern)
		status := strconv.Itoa(rec.statusCode)
		m.requestsTotal.WithLabelValues(r.Method, route, status).Inc()
		m.requestDuration.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
	})
}

// routeLabel strips the method prefix Go 1.22 mux patterns carry, keeping just the path.
func routeLabel(pattern string) string {
	if pattern == "" {
		return "unmatched"
	}
	if _, path, found := strings.Cut(pattern, " "); found {
		return path
	}
	return pattern
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}
