// Package server wires the payment HTTP API onto the shared runtime.
package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/eventstore"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/gateway"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/handler"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/repository"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/service"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/database"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/httpserver"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/metrics"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"
)

// paymentGatewayMaxAmountCents bounds what the stub payment gateway will approve, in cents.
const paymentGatewayMaxAmountCents = 1_000_000

// Server runs the payment HTTP API on the shared runtime.
type Server struct {
	runtime *httpserver.Server
}

// Options configures the payment server.
type Options struct {
	Config  *config.Config
	Logger  *zap.Logger
	Metrics prometheus.Registerer
	DB      *database.DB
}

// New builds the payment HTTP server: health checks, metrics and the shared middleware chain.
func New(opts Options) *Server {
	registerer := opts.Metrics
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	checks := make(map[string]httpserver.Check)
	if opts.DB != nil {
		checks["database"] = opts.DB.PingContext
	}
	if len(opts.Config.Kafka.Brokers) > 0 {
		brokers := opts.Config.Kafka.Brokers
		checks["kafka"] = func(ctx context.Context) error { return events.Healthy(ctx, brokers) }
	}

	mux := http.NewServeMux()
	httpserver.NewHealthHandlers(opts.Config.Service.Name, checks).Register(mux)

	if opts.DB != nil {
		repo := eventstore.NewRepository(opts.DB.DB)
		gatewayClient := gateway.NewStubClient(gateway.Config{MaxAmountCents: paymentGatewayMaxAmountCents})
		paymentService := service.NewPaymentService(repo, gatewayClient)
		statusReader := repository.NewPaymentStatusRepository(opts.DB.DB)
		eventReader := eventstore.NewStore(opts.DB.DB)

		paymentsHandler := handler.NewPaymentsHandler(paymentService, statusReader, eventReader, opts.Logger)
		mux.HandleFunc("POST /api/v1/payments", paymentsHandler.Process)
		mux.HandleFunc("POST /api/v1/payments/{id}/refund", paymentsHandler.Refund)
		mux.HandleFunc("GET /api/v1/payments/{id}", paymentsHandler.Get)
		mux.HandleFunc("GET /api/v1/payments", paymentsHandler.List)
		mux.HandleFunc("GET /api/v1/payments/{id}/events", paymentsHandler.Events)
	}

	httpMetrics := metrics.NewHTTPMetrics(registerer, "payment")
	chain := middleware.Chain(
		middleware.Recovery(opts.Logger),
		middleware.RequestID,
		middleware.Logging(opts.Logger),
	)
	wrappedHandler := chain(httpMetrics.Middleware(mux))
	// otelhttp.NewHandler opens a server span for every request; the span name uses the route
	// rather than the full path, so requests for different payment ids do not each mint a
	// distinct span name.
	tracedHandler := otelhttp.NewHandler(wrappedHandler, "payment",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + routePath(r.URL.Path)
		}),
	)

	outer := http.NewServeMux()
	outer.Handle("/metrics", promhttp.Handler())
	outer.Handle("/", tracedHandler)

	runtime := httpserver.New(httpserver.Options{
		Addr:    opts.Config.Server.Host + ":" + opts.Config.Server.Port,
		Handler: outer,
		Logger:  opts.Logger,
	})

	return &Server{runtime: runtime}
}

// Start begins serving and blocks until the server stops or fails.
func (s *Server) Start() error {
	return s.runtime.Start()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	return s.runtime.Stop(ctx)
}

// Handler returns the server's top-level handler, useful for tests.
func (s *Server) Handler() http.Handler {
	return s.runtime.Handler()
}

// routePath collapses path to its route: the leading "/api/v1/<resource>" segments, or the
// top-level segment for anything shorter (like "/health"), dropping identifiers and
// sub-resources beneath it so span names stay low cardinality.
func routePath(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	const maxRouteSegments = 3
	if len(segments) > maxRouteSegments {
		segments = segments[:maxRouteSegments]
	}
	return "/" + strings.Join(segments, "/")
}
