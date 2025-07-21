package logger

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	*zap.Logger
	sugar *zap.SugaredLogger
}

type Config struct {
	Level       string   `json:"level"`       // debug, info, warn, error
	Environment string   `json:"environment"` // development, production
	Service     string   `json:"service"`
	Version     string   `json:"version"`
	OutputPaths []string `json:"output_paths"`
}

func New(config Config) (*Logger, error) {
	var zapConfig zap.Config

	if config.Environment == "production" {
		zapConfig = zap.NewProductionConfig()
		zapConfig.EncoderConfig.TimeKey = "timestamp"
		zapConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		zapConfig = zap.NewDevelopmentConfig()
		zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// Set log level
	level, err := zapcore.ParseLevel(config.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}
	zapConfig.Level = zap.NewAtomicLevelAt(level)

	// Set output paths
	if len(config.OutputPaths) > 0 {
		zapConfig.OutputPaths = config.OutputPaths
	}

	// Add initial fields
	zapConfig.InitialFields = map[string]interface{}{
		"service": config.Service,
		"version": config.Version,
	}

	logger, err := zapConfig.Build()
	if err != nil {
		return nil, err
	}

	return &Logger{
		Logger: logger,
		sugar:  logger.Sugar(),
	}, nil
}

func (l *Logger) WithTracing(ctx context.Context) *zap.Logger {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		return l.Logger.With(
			zap.String("trace_id", span.SpanContext().TraceID().String()),
			zap.String("span_id", span.SpanContext().SpanID().String()),
		)
	}
	return l.Logger
}

func (l *Logger) WithCorrelationID(correlationID string) *zap.Logger {
	return l.Logger.With(zap.String("correlation_id", correlationID))
}

func (l *Logger) WithUserID(userID string) *zap.Logger {
	return l.Logger.With(zap.String("user_id", userID))
}

func (l *Logger) WithRequestID(requestID string) *zap.Logger {
	return l.Logger.With(zap.String("request_id", requestID))
}

func (l *Logger) Sugar() *zap.SugaredLogger {
	return l.sugar
}

func (l *Logger) Sync() error {
	return l.Logger.Sync()
}

func DefaultConfig() Config {
	return Config{
		Level:       "info",
		Environment: "development",
		Service:     "eventflow-service",
		Version:     "1.0.0",
		OutputPaths: []string{"stdout"},
	}
}

func DefaultProductionConfig() Config {
	return Config{
		Level:       "info",
		Environment: "production",
		Service:     "eventflow-service",
		Version:     "1.0.0",
		OutputPaths: []string{"stdout"},
	}
}
