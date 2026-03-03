package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/handler"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Server represents the HTTP server
type Server struct {
	config      *config.Config
	logger      *zap.Logger
	httpServer  *http.Server
	rateLimiter *handler.RateLimiter
	metrics     *handler.Metrics
	router      *handler.Router
}

// Options contains options for creating a new server
type Options struct {
	Config  *config.Config
	Logger  *zap.Logger
	Metrics *handler.Metrics
}

// NewServer creates a new server instance
func NewServer(opts Options) (*Server, error) {
	// Create rate limiter
	rateLimiter := handler.NewRateLimiter(
		opts.Config.RateLimit.RequestsPerMinute,
		time.Duration(opts.Config.RateLimit.WindowDuration)*time.Second,
	)

	// Use provided metrics or create new ones
	metrics := opts.Metrics
	if metrics == nil {
		metrics = handler.NewMetrics()
	}

	// Create router
	router, err := handler.NewRouter(opts.Config, opts.Logger, metrics, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	// Setup main handler with middleware chain
	mux := http.NewServeMux()

	// Add metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// Setup routes
	router.SetupRoutes()

	// Apply middleware chain to router
	var finalHandler http.Handler = router
	finalHandler = handler.JWTMiddleware(opts.Config.JWTSecret, opts.Logger, metrics)(finalHandler)
	finalHandler = handler.RateLimitMiddleware(rateLimiter, metrics)(finalHandler)
	finalHandler = recordRequestMetrics(metrics)(finalHandler)

	// Mount the router with middleware chain
	mux.Handle("/", finalHandler)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         opts.Config.Server.Host + ":" + opts.Config.Server.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &Server{
		config:      opts.Config,
		logger:      opts.Logger,
		httpServer:  httpServer,
		rateLimiter: rateLimiter,
		metrics:     metrics,
		router:      router,
	}, nil
}

// recordRequestMetrics wraps every request, recording it with Metrics.RecordRequest
// regardless of which downstream layer (rate limiting, JWT, routing) produced the response.
func recordRequestMetrics(metrics *handler.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(recorder, r)

			metrics.RecordRequest(r.Method, r.URL.Path, recorder.statusCode, time.Since(start))
		})
	}
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

// Start starts the HTTP server
func (s *Server) Start() error {
	s.logger.Info("Starting API Gateway server",
		zap.String("address", s.httpServer.Addr),
		zap.String("version", s.config.Service.Version))

	return s.httpServer.ListenAndServe()
}

// Stop gracefully stops the server
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("Stopping API Gateway server")

	// Close rate limiter cleanup goroutine
	if s.rateLimiter != nil {
		s.rateLimiter.Close()
	}

	// Shutdown HTTP server
	return s.httpServer.Shutdown(ctx)
}

// GetHTTPServer returns the underlying HTTP server
func (s *Server) GetHTTPServer() *http.Server {
	return s.httpServer
}

// GetRateLimiter returns the rate limiter
func (s *Server) GetRateLimiter() *handler.RateLimiter {
	return s.rateLimiter
}

// GetMetrics returns the metrics instance
func (s *Server) GetMetrics() *handler.Metrics {
	return s.metrics
}

// GetRouter returns the router instance
func (s *Server) GetRouter() *handler.Router {
	return s.router
}
