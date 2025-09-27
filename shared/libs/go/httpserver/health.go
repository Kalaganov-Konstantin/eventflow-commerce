package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Check reports whether a dependency the service relies on is healthy.
type Check func(ctx context.Context) error

// HealthStatus is the JSON body returned by the health and readiness endpoints.
type HealthStatus struct {
	Status    string            `json:"status"`
	Service   string            `json:"service"`
	Timestamp time.Time         `json:"timestamp"`
	Details   map[string]string `json:"details,omitempty"`
}

// HealthHandlers serves the health, liveness and readiness endpoints for a service.
type HealthHandlers struct {
	service string
	checks  map[string]Check
}

// NewHealthHandlers builds handlers that report as the given service name and run checks on readiness.
func NewHealthHandlers(service string, checks map[string]Check) *HealthHandlers {
	return &HealthHandlers{service: service, checks: checks}
}

// Register attaches the health, liveness and readiness routes to mux.
func (h *HealthHandlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /health/live", h.Live)
	mux.HandleFunc("GET /health/ready", h.Ready)
}

// Health always reports healthy; it signals the process is up.
func (h *HealthHandlers) Health(w http.ResponseWriter, _ *http.Request) {
	writeHealthStatus(w, http.StatusOK, HealthStatus{Status: "healthy", Service: h.service, Timestamp: time.Now()})
}

// Live always reports healthy; it backs the liveness probe.
func (h *HealthHandlers) Live(w http.ResponseWriter, _ *http.Request) {
	writeHealthStatus(w, http.StatusOK, HealthStatus{Status: "healthy", Service: h.service, Timestamp: time.Now()})
}

// Ready runs the registered checks and reports 503 with the failing ones if any fail.
func (h *HealthHandlers) Ready(w http.ResponseWriter, r *http.Request) {
	details := make(map[string]string)

	for name, check := range h.checks {
		if err := check(r.Context()); err != nil {
			details[name] = err.Error()
		}
	}

	if len(details) == 0 {
		writeHealthStatus(w, http.StatusOK, HealthStatus{Status: "healthy", Service: h.service, Timestamp: time.Now()})
		return
	}

	writeHealthStatus(w, http.StatusServiceUnavailable, HealthStatus{
		Status:    "unhealthy",
		Service:   h.service,
		Timestamp: time.Now(),
		Details:   details,
	})
}

func writeHealthStatus(w http.ResponseWriter, code int, status HealthStatus) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(status)
}
