package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/config"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Backend names used both as proxy map keys and metric labels.
const (
	backendOrder        = "order"
	backendPayment      = "payment"
	backendInventory    = "inventory"
	backendNotification = "notification"
)

// correlationIDHeader carries the correlation ID that ties an HTTP request to
// the chain of Kafka events it triggers downstream.
const correlationIDHeader = "X-Correlation-ID"

// defaultReadinessTimeout bounds how long a single backend health check may
// take while answering /health/ready.
const defaultReadinessTimeout = 2 * time.Second

// Router handles HTTP routing and proxying
type Router struct {
	config           *config.Config
	logger           *zap.Logger
	metrics          *Metrics
	mux              *http.ServeMux
	startTime        time.Time
	proxies          map[string]*backendProxy
	httpClient       *http.Client
	readinessTimeout time.Duration
}

// statusRecorder captures the status code written to a ResponseWriter so it
// can be reported in metrics after the handler returns.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.statusCode = code
	s.ResponseWriter.WriteHeader(code)
}

// backendProxy pairs a parsed backend target with its pre-built reverse proxy.
type backendProxy struct {
	name   string
	target *url.URL
	proxy  *httputil.ReverseProxy
}

// ErrorResponse defines the structure for error responses
type ErrorResponse struct {
	Error     string            `json:"error"`
	Code      string            `json:"code"`
	RequestID string            `json:"request_id,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}

// NewRouter creates a new router instance, building one reverse proxy per
// backend service up front. It returns an error if a configured service URL
// cannot be parsed. metrics may be nil, in which case no metrics are recorded.
func NewRouter(cfg *config.Config, logger *zap.Logger, metrics *Metrics, startTime time.Time) (*Router, error) {
	r := &Router{
		config:           cfg,
		logger:           logger,
		metrics:          metrics,
		mux:              http.NewServeMux(),
		startTime:        startTime,
		proxies:          make(map[string]*backendProxy, 4),
		httpClient:       &http.Client{},
		readinessTimeout: defaultReadinessTimeout,
	}

	backends := map[string]string{
		backendOrder:        cfg.OrderServiceURL,
		backendPayment:      cfg.PaymentServiceURL,
		backendInventory:    cfg.InventoryServiceURL,
		backendNotification: cfg.NotificationServiceURL,
	}

	for name, rawURL := range backends {
		bp, err := r.newBackendProxy(name, rawURL)
		if err != nil {
			return nil, err
		}
		r.proxies[name] = bp
	}

	return r, nil
}

// newBackendProxy parses a backend service URL and builds its reverse proxy
// once, wiring header propagation into the proxy's Director.
func (r *Router) newBackendProxy(name, rawURL string) (*backendProxy, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid %s service url %q: %w", name, rawURL, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalPath := req.URL.Path
		baseDirector(req)
		r.setProxyHeaders(req, originalPath, target.Host)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		r.proxyErrorHandler(w, req, err, name)
	}

	return &backendProxy{name: name, target: target, proxy: proxy}, nil
}

// SetupRoutes configures all routes
func (r *Router) SetupRoutes() {
	// Health check endpoints
	r.mux.HandleFunc("/health", r.healthCheck)
	r.mux.HandleFunc("/health/live", r.livenessCheck)
	r.mux.HandleFunc("/health/ready", r.readinessCheck)

	// API routes with service-specific prefixes; the backend receives the full path
	r.mux.HandleFunc("/api/v1/orders/", r.createProxyHandler(backendOrder))
	r.mux.HandleFunc("/api/v1/payments/", r.createProxyHandler(backendPayment))
	r.mux.HandleFunc("/api/v1/inventory/", r.createProxyHandler(backendInventory))
	r.mux.HandleFunc("/api/v1/products/", r.createProxyHandler(backendInventory))
	r.mux.HandleFunc("/api/v1/notifications/", r.createProxyHandler(backendNotification))
}

// ServeHTTP implements http.Handler
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// healthCheck handles health check requests
func (r *Router) healthCheck(w http.ResponseWriter, _ *http.Request) {
	uptime := time.Since(r.startTime)

	status := HealthStatus{
		Status:    "healthy",
		Service:   r.getServiceName(),
		Timestamp: time.Now(),
		Details: map[string]string{
			"version": r.getServiceVersion(),
			"uptime":  uptime.String(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(status); err != nil {
		r.logger.Error("Failed to encode health check response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// livenessCheck answers whether the process is up. It never checks backends.
func (r *Router) livenessCheck(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "alive"}); err != nil {
		r.logger.Error("Failed to encode liveness response", zap.Error(err))
	}
}

// readinessCheck answers whether every backend service is reachable, polling
// each one's /health endpoint with a short timeout.
func (r *Router) readinessCheck(w http.ResponseWriter, req *http.Request) {
	names := []string{backendOrder, backendPayment, backendInventory, backendNotification}
	unavailable := make([]string, 0, len(names))

	for _, name := range names {
		if !r.backendHealthy(req.Context(), name) {
			unavailable = append(unavailable, name)
		}
	}

	w.Header().Set("Content-Type", "application/json")

	if len(unavailable) > 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"status":      "not_ready",
			"unavailable": unavailable,
		}); err != nil {
			r.logger.Error("Failed to encode readiness response", zap.Error(err))
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ready"}); err != nil {
		r.logger.Error("Failed to encode readiness response", zap.Error(err))
	}
}

// backendHealthy calls the named backend's /health endpoint with a bounded timeout.
func (r *Router) backendHealthy(ctx context.Context, name string) bool {
	bp, ok := r.proxies[name]
	if !ok {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, r.readinessTimeout)
	defer cancel()

	healthReq, err := http.NewRequestWithContext(ctx, http.MethodGet, bp.target.String()+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := r.httpClient.Do(healthReq)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func (r *Router) getServiceName() string {
	if r.config != nil && r.config.Service.Name != "" {
		return r.config.Service.Name
	}
	return "api-gateway"
}

func (r *Router) getServiceVersion() string {
	if r.config != nil && r.config.Service.Version != "" {
		return r.config.Service.Version
	}
	return "unknown"
}

// createProxyHandler creates a reverse proxy handler for a backend service
func (r *Router) createProxyHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.proxyToService(w, req, name)
	}
}

// proxyToService forwards the request through the pre-built reverse proxy for
// the named backend. The backend receives the full request path.
func (r *Router) proxyToService(w http.ResponseWriter, req *http.Request, name string) {
	bp := r.proxies[name]

	ensureCorrelationID(w, req)

	// Set timeout context
	timeout := time.Duration(r.config.ProxyTimeout) * time.Second
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}

	start := time.Now()
	recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

	bp.proxy.ServeHTTP(recorder, req)

	if r.metrics != nil {
		r.metrics.RecordProxyRequest(name, req.Method, recorder.statusCode, time.Since(start))
	}
}

// ensureCorrelationID returns the request's correlation ID, generating one if
// the caller did not supply it, and mirrors it onto both the outgoing request
// (so the backend receives it) and the response (so the caller can log it).
func ensureCorrelationID(w http.ResponseWriter, req *http.Request) string {
	correlationID := req.Header.Get(correlationIDHeader)
	if correlationID == "" {
		correlationID = uuid.New().String()
		req.Header.Set(correlationIDHeader, correlationID)
	}
	w.Header().Set(correlationIDHeader, correlationID)
	return correlationID
}

// setProxyHeaders sets standard proxy headers
func (r *Router) setProxyHeaders(req *http.Request, originalPath, targetHost string) {
	// Set forwarded headers
	if clientIP := r.getClientIP(req); clientIP != "" {
		req.Header.Set("X-Forwarded-For", clientIP)
		if req.Header.Get("X-Real-IP") == "" {
			req.Header.Set("X-Real-IP", clientIP)
		}
	}

	// Set forwarded protocol
	if req.URL.Scheme != "" {
		req.Header.Set("X-Forwarded-Proto", req.URL.Scheme)
	} else {
		req.Header.Set("X-Forwarded-Proto", "http")
	}

	// Set original path
	req.Header.Set("X-Original-Path", originalPath)

	// Set forwarded host
	req.Header.Set("X-Forwarded-Host", req.Host)

	// Set target host as Host header for backend service
	req.Host = targetHost
}

// getClientIP extracts the real client IP from the request
func (r *Router) getClientIP(req *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if r.isValidIP(ip) {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	if xri := req.Header.Get("X-Real-IP"); xri != "" {
		if r.isValidIP(xri) {
			return xri
		}
	}

	// Fall back to RemoteAddr
	if req.RemoteAddr != "" {
		// RemoteAddr includes port, strip it
		if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
			if r.isValidIP(host) {
				return host
			}
		}
		// If parsing fails, try to extract IP manually
		if idx := strings.LastIndex(req.RemoteAddr, ":"); idx != -1 {
			ip := req.RemoteAddr[:idx]
			if r.isValidIP(ip) {
				return ip
			}
		}
	}

	return "unknown"
}

// isValidIP validates if the string is a valid IP address
func (r *Router) isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// proxyErrorHandler handles proxy errors
func (r *Router) proxyErrorHandler(w http.ResponseWriter, req *http.Request, err error, targetService string) {
	r.logger.Error("Proxy request failed",
		zap.String("url", req.URL.String()),
		zap.String("method", req.Method),
		zap.Error(err))

	// Determine error type and status code
	var statusCode int
	var errorCode string

	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "connection refused"):
		statusCode = http.StatusServiceUnavailable
		errorCode = "SERVICE_UNAVAILABLE"
	case strings.Contains(errStr, "timeout"):
		statusCode = http.StatusGatewayTimeout
		errorCode = "GATEWAY_TIMEOUT"
	case strings.Contains(errStr, "no such host"):
		statusCode = http.StatusBadGateway
		errorCode = "INVALID_HOST"
	default:
		statusCode = http.StatusBadGateway
		errorCode = "PROXY_ERROR"
	}

	if r.metrics != nil {
		r.metrics.RecordProxyError(targetService, errorCode)
	}

	// Create error response
	errorResponse := ErrorResponse{
		Error: "Failed to proxy request to backend service",
		Code:  errorCode,
		Details: map[string]string{
			"target_url": req.URL.String(),
			"method":     req.Method,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if encodeErr := json.NewEncoder(w).Encode(errorResponse); encodeErr != nil {
		r.logger.Error("Failed to encode error response", zap.Error(encodeErr))
		// Fallback to plain text error
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
