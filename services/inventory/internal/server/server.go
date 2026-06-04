// Package server wires the inventory HTTP API onto the shared runtime.
package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/handler"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/repository"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/service"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/cache"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/database"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/httpserver"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/metrics"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"
)

// Server runs the inventory HTTP API on the shared runtime.
type Server struct {
	runtime *httpserver.Server
}

// Options configures the inventory server.
type Options struct {
	Config  *config.Config
	Logger  *zap.Logger
	Metrics prometheus.Registerer
	DB      *database.DB
	// Redis is optional: a nil client means the service runs without a product cache.
	Redis *database.RedisClient
	// CacheMetrics is optional: a nil value means product cache reads are not recorded as hits
	// or misses.
	CacheMetrics *cache.Metrics
}

// New builds the inventory HTTP server: health checks, metrics and the shared middleware chain.
func New(opts Options) *Server {
	registerer := opts.Metrics
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	checks := make(map[string]httpserver.Check)
	if opts.DB != nil {
		checks["database"] = opts.DB.PingContext
	}
	if opts.Redis != nil {
		checks["redis"] = func(ctx context.Context) error { return opts.Redis.Ping(ctx).Err() }
	}

	mux := http.NewServeMux()
	httpserver.NewHealthHandlers(opts.Config.Service.Name, checks).Register(mux)

	if opts.DB != nil {
		stockRepo := repository.NewStockRepository(opts.DB.DB)

		var productCache service.ProductCache
		if opts.Redis != nil {
			c := cache.New(opts.Redis, 0)
			if opts.CacheMetrics != nil {
				c.SetMetrics(opts.CacheMetrics, "product")
			}
			productCache = c
		}
		productService := service.NewProductService(repository.NewProductRepository(opts.DB.DB), productCache)

		productsHandler := handler.NewProductsHandler(productService, stockRepo, opts.Logger)
		mux.HandleFunc("GET /api/v1/products", productsHandler.List)
		mux.HandleFunc("GET /api/v1/products/{id}", productsHandler.Get)
		mux.HandleFunc("GET /api/v1/inventory/{product_id}", productsHandler.Inventory)

		reservationsHandler := handler.NewReservationsHandler(stockRepo, opts.Logger)
		mux.HandleFunc("POST /api/v1/inventory/reservations", reservationsHandler.Reserve)
		mux.HandleFunc("DELETE /api/v1/inventory/reservations/{order_id}", reservationsHandler.Release)
	}

	httpMetrics := metrics.NewHTTPMetrics(registerer, "inventory")
	chain := middleware.Chain(
		middleware.Recovery(opts.Logger),
		middleware.RequestID,
		middleware.Logging(opts.Logger),
	)
	wrappedHandler := chain(httpMetrics.Middleware(mux))
	// otelhttp.NewHandler opens a server span for every request; the span name uses the route
	// rather than the full path, so requests for different product ids do not each mint a
	// distinct span name.
	tracedHandler := otelhttp.NewHandler(wrappedHandler, "inventory",
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
