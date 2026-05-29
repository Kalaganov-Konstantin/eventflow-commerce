package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/api-gateway/internal/server"
	sharedlogger "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/logger"
	sharedtracing "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/tracing"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger from configuration
	appLogger, err := sharedlogger.New(sharedlogger.Config{
		Level:       cfg.Logger.Level,
		Environment: cfg.Logger.Environment,
		Service:     cfg.Service.Name,
		Version:     cfg.Service.Version,
		OutputPaths: cfg.Logger.OutputPaths,
	})
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer func() { _ = appLogger.Sync() }()
	logger := appLogger.Logger

	logger.Info("Starting API Gateway service...")

	logger.Info("Configuration loaded successfully",
		zap.String("host", cfg.Server.Host),
		zap.String("port", cfg.Server.Port),
		zap.String("version", cfg.Service.Version))

	shutdownTracing, err := sharedtracing.Init(context.Background(), sharedtracing.Config{
		ServiceName:    cfg.Service.Name,
		ServiceVersion: cfg.Service.Version,
		Endpoint:       cfg.Jaeger.Endpoint,
	})
	if err != nil {
		logger.Fatal("Failed to initialize tracing", zap.Error(err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			logger.Error("Failed to shut down tracing", zap.Error(err))
		}
	}()

	// Create and start server
	srv, err := server.NewServer(server.Options{
		Config: cfg,
		Logger: logger,
	})
	if err != nil {
		logger.Fatal("Failed to create server", zap.Error(err))
	}

	// Setup graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed to start", zap.Error(err))
		}
	}()

	logger.Info("API Gateway started successfully")

	// Wait for interrupt signal
	<-done
	logger.Info("API Gateway is shutting down...")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown server gracefully
	if err := srv.Stop(ctx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
	} else {
		logger.Info("API Gateway stopped gracefully")
	}
}
