// Package tracing configures OpenTelemetry trace export for a service.
package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// Config configures Init.
type Config struct {
	ServiceName    string
	ServiceVersion string
	// Endpoint is the OTLP HTTP collector base URL, e.g. "http://jaeger:4318". An empty
	// Endpoint disables trace export instead of failing startup.
	Endpoint string
}

// Init installs the global W3C tracecontext and baggage propagators, and, when Config.Endpoint is
// set, an OTLP HTTP exporter sampled according to OTEL_TRACES_SAMPLER. It returns a shutdown
// function that flushes and stops the exporter; callers must invoke it before the process exits.
// An empty Config.Endpoint disables export rather than returning an error: the propagators are
// still installed and Init returns a no-op shutdown.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}

	res := resource.NewSchemaless(
		semconv.ServiceNameKey.String(cfg.ServiceName),
		semconv.ServiceVersionKey.String(cfg.ServiceVersion),
	)

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(samplerFromEnv()),
	)
	otel.SetTracerProvider(provider)

	return provider.Shutdown, nil
}

// samplerFromEnv builds a Sampler from the standard OTEL_TRACES_SAMPLER and
// OTEL_TRACES_SAMPLER_ARG environment variables, defaulting to always sampling behind the parent's
// own decision when unset or unrecognized.
func samplerFromEnv() sdktrace.Sampler {
	switch os.Getenv("OTEL_TRACES_SAMPLER") {
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(samplerRatioFromEnv())
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(samplerRatioFromEnv()))
	default: // "", "parentbased_always_on" and anything unrecognized
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

// samplerRatioFromEnv reads OTEL_TRACES_SAMPLER_ARG as the sampling ratio, falling back to
// sampling everything when it is missing or not a valid fraction in [0, 1].
func samplerRatioFromEnv() float64 {
	ratio, err := strconv.ParseFloat(os.Getenv("OTEL_TRACES_SAMPLER_ARG"), 64)
	if err != nil || ratio < 0 || ratio > 1 {
		return 1
	}
	return ratio
}
