package handler

import (
	"context"
	"net/http"
	"time"
)

// RateLimiterInterface defines the contract for rate limiting
type RateLimiterInterface interface {
	Allow(clientID string) bool
	Close()
}

// RouterInterface defines the contract for routing
type RouterInterface interface {
	SetupRoutes()
	ServeHTTP(w http.ResponseWriter, req *http.Request)
}

// ProxyInterface defines the contract for proxying requests
type ProxyInterface interface {
	ProxyRequest(w http.ResponseWriter, req *http.Request, targetURL, pathPrefix string) error
}

// HealthCheckerInterface defines the contract for health checking
type HealthCheckerInterface interface {
	CheckHealth(ctx context.Context) (HealthStatus, error)
}

// HealthStatus represents the health status of a service
type HealthStatus struct {
	Status    string            `json:"status"`
	Service   string            `json:"service"`
	Timestamp time.Time         `json:"timestamp"`
	Details   map[string]string `json:"details,omitempty"`
}

// Ensure our concrete types implement the interfaces
var (
	_ RateLimiterInterface = (*RateLimiter)(nil)
	_ RouterInterface      = (*Router)(nil)
)
