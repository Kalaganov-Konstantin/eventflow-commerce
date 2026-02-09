package main

import (
	"context"
	stderrors "errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/client"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/config"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/consumer"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/repository"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/server"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/service"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/database"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	sharedlogger "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/logger"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/outbox"
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

	appLogger.Info("Starting order service",
		zap.String("host", cfg.Server.Host),
		zap.String("port", cfg.Server.Port),
		zap.String("version", cfg.Service.Version))

	db, err := database.NewPostgresConnection(database.PostgresConfig{
		URL:          cfg.DatabaseURL,
		MaxOpenConns: cfg.DatabasePool.MaxOpenConns,
		MaxIdleConns: cfg.DatabasePool.MaxIdleConns,
		MaxLifetime:  cfg.DatabasePool.MaxLifetime,
	})
	if err != nil {
		appLogger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer func() { _ = db.Close() }()

	srv := server.New(server.Options{
		Config: cfg,
		Logger: appLogger.Logger,
		DB:     db,
	})

	publisher := events.NewPublisher(events.KafkaConfig{Brokers: cfg.Kafka.Brokers})
	relay := outbox.NewRelay(db.DB, publisher, appLogger.Logger, cfg.Outbox.RelayInterval, cfg.Outbox.RelayBatchSize)
	relay.Start(context.Background())

	inventoryClient := client.NewInventoryClient(cfg.InventoryServiceURL, cfg.InventoryClient.Timeout)
	orderService := service.NewOrderService(
		repository.NewOrderRepository(db.DB), db.DB, repository.NewSagaRepository(db.DB), inventoryClient)
	processedStore := events.NewProcessedStore(db.DB)
	paymentsSubscriber := events.NewSubscriber(events.KafkaConfig{
		Brokers:  cfg.Kafka.Brokers,
		GroupID:  cfg.Kafka.GroupID,
		DLQTopic: events.DLQTopic(events.PaymentsTopic),
	}, events.PaymentsTopic, appLogger.Logger)
	paymentsConsumer := consumer.NewPaymentsConsumer(paymentsSubscriber, db.DB, processedStore, orderService, appLogger.Logger)

	consumerCtx, stopConsumer := context.WithCancel(context.Background())
	go func() {
		if err := paymentsConsumer.Start(consumerCtx); err != nil && !stderrors.Is(err, context.Canceled) {
			appLogger.Error("payments consumer stopped", zap.Error(err))
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			appLogger.Fatal("Server failed to start", zap.Error(err))
		}
	}()

	<-done
	appLogger.Info("Order service is shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		appLogger.Error("Server forced to shutdown", zap.Error(err))
	} else {
		appLogger.Info("Order service stopped gracefully")
	}

	stopConsumer()
	if err := paymentsSubscriber.Close(); err != nil {
		appLogger.Error("Failed to close payments subscriber", zap.Error(err))
	}

	relay.Stop()
	if err := publisher.Close(); err != nil {
		appLogger.Error("Failed to close kafka publisher", zap.Error(err))
	}
}
