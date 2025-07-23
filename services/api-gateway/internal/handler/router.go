package handler

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/config"
	"go.uber.org/zap"
)

// Router handles HTTP routing and proxying
type Router struct {
	config    *config.Config
	logger    *zap.Logger
	mux       *http.ServeMux
	startTime time.Time
}

// ErrorResponse defines the structure for error responses
type ErrorResponse struct {
	Error     string            `json:"error"`
	Code      string            `json:"code"`
	RequestID string            `json:"request_id,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}

// NewRouter creates a new router instance
func NewRouter(cfg *config.Config, logger *zap.Logger, startTime time.Time) *Router {
	return &Router{
		config:    cfg,
		logger:    logger,
		mux:       http.NewServeMux(),
		startTime: startTime,
	}
}

// SetupRoutes configures all routes
func (r *Router) SetupRoutes() {
	// Health check endpoint
	r.mux.HandleFunc("/health", r.healthCheck)

	// API routes with service-specific prefixes
	r.mux.HandleFunc("/api/v1/orders/", r.createProxyHandler(r.config.OrderServiceURL, "/api/v1/orders"))
	r.mux.HandleFunc("/api/v1/payments/", r.createProxyHandler(r.config.PaymentServiceURL, "/api/v1/payments"))
	r.mux.HandleFunc("/api/v1/inventory/", r.createProxyHandler(r.config.InventoryServiceURL, "/api/v1/inventory"))
	r.mux.HandleFunc("/api/v1/products/", r.createProxyHandler(r.config.InventoryServiceURL, "/api/v1/products"))
	r.mux.HandleFunc("/api/v1/notifications/", r.createProxyHandler(r.config.NotificationServiceURL, "/api/v1/notifications"))
}

// ServeHTTP implements http.Handler
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// healthCheck handles health check requests
func (r *Router) healthCheck(w http.ResponseWriter, req *http.Request) {
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

// createProxyHandler creates a reverse proxy handler for a service
func (r *Router) createProxyHandler(targetURL, prefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.proxyToService(w, req, targetURL, prefix)
	}
}

// proxyToService handles proxying requests to backend services
func (r *Router) proxyToService(w http.ResponseWriter, req *http.Request, targetURL, prefix string) {
	// Parse target URL
	target, err := url.Parse(targetURL)
	if err != nil {
		r.logger.Error("Failed to parse target URL", zap.String("url", targetURL), zap.Error(err))
		r.proxyErrorHandler(w, req, err)
		return
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = r.proxyErrorHandler

	// Modify request
	originalPath := req.URL.Path
	req.URL.Host = target.Host
	req.URL.Scheme = target.Scheme

	// Strip prefix from path
	if strings.HasPrefix(req.URL.Path, prefix) {
		req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
	}

	// Set proxy headers
	r.setProxyHeaders(req, originalPath, target.Host)

	// Set timeout context
	timeout := time.Duration(r.config.ProxyTimeout) * time.Second
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}

	// Serve the request
	proxy.ServeHTTP(w, req)
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
func (r *Router) proxyErrorHandler(w http.ResponseWriter, req *http.Request, err error) {
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
