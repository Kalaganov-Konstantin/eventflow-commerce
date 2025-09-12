// Package httpserver provides a shared HTTP server runtime with graceful shutdown.
package httpserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	defaultReadTimeout  = 15 * time.Second
	defaultWriteTimeout = 15 * time.Second
	defaultIdleTimeout  = 60 * time.Second
)

// Options configures a Server.
type Options struct {
	Addr         string
	Handler      http.Handler
	Logger       *zap.Logger
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// Server wraps http.Server with graceful shutdown and default timeouts.
type Server struct {
	httpServer *http.Server
	logger     *zap.Logger

	mu       sync.Mutex
	listener net.Listener
}

// New builds a Server from Options, applying default timeouts when unset.
func New(opts Options) *Server {
	readTimeout := opts.ReadTimeout
	if readTimeout == 0 {
		readTimeout = defaultReadTimeout
	}
	writeTimeout := opts.WriteTimeout
	if writeTimeout == 0 {
		writeTimeout = defaultWriteTimeout
	}
	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = defaultIdleTimeout
	}

	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Server{
		httpServer: &http.Server{
			Addr:         opts.Addr,
			Handler:      opts.Handler,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			IdleTimeout:  idleTimeout,
		},
		logger: logger,
	}
}

// Start listens on the configured address and serves until Stop is called or an error occurs.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.httpServer.Addr, err)
	}

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	s.logger.Info("starting http server", zap.String("address", ln.Addr().String()))
	return s.httpServer.Serve(ln)
}

// Stop gracefully shuts down the server, waiting for in-flight requests to finish.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("stopping http server")
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the bound listener address, or the configured address if Start has not run yet.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.httpServer.Addr
}

// Handler returns the handler the server was configured with.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}
