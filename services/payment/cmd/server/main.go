package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/server"
	sharedlogger "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/logger"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

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

	appLogger.Info("Starting payment service",
		zap.String("host", cfg.Server.Host),
		zap.String("port", cfg.Server.Port),
		zap.String("version", cfg.Service.Version))

	srv := server.New(server.Options{
		Config: cfg,
		Logger: appLogger.Logger,
	})

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			appLogger.Fatal("Server failed to start", zap.Error(err))
		}
	}()

	<-done
	appLogger.Info("Payment service is shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		appLogger.Error("Server forced to shutdown", zap.Error(err))
	} else {
		appLogger.Info("Payment service stopped gracefully")
	}
}
