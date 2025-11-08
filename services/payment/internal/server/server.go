// Package server wires the payment HTTP API onto the shared runtime.
package server

import (
	"context"
	"net/http"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/eventstore"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/gateway"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/handler"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/service"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/database"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/httpserver"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/metrics"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	var checks map[string]httpserver.Check
	if opts.DB != nil {
		checks = map[string]httpserver.Check{
			"database": opts.DB.PingContext,
		}
	}

	mux := http.NewServeMux()
	httpserver.NewHealthHandlers(opts.Config.Service.Name, checks).Register(mux)

	if opts.DB != nil {
		repo := eventstore.NewRepository(opts.DB.DB)
		gatewayClient := gateway.NewStubClient(gateway.Config{MaxAmountCents: paymentGatewayMaxAmountCents})
		paymentService := service.NewPaymentService(repo, gatewayClient)

		paymentsHandler := handler.NewPaymentsHandler(paymentService, opts.Logger)
		mux.HandleFunc("POST /api/v1/payments", paymentsHandler.Process)
	}

	httpMetrics := metrics.NewHTTPMetrics(registerer, "payment")
	chain := middleware.Chain(
		middleware.Recovery(opts.Logger),
		middleware.RequestID,
		middleware.Logging(opts.Logger),
	)
	handler := chain(httpMetrics.Middleware(mux))

	outer := http.NewServeMux()
	outer.Handle("/metrics", promhttp.Handler())
	outer.Handle("/", handler)

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
