package tracing

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestInit_EmptyEndpointDisablesExport(t *testing.T) {
	shutdown, err := Init(context.Background(), Config{ServiceName: "svc", ServiceVersion: "1.0.0"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown() error = %v, want nil for a disabled exporter", err)
	}
}

func TestInit_ConfiguresW3CPropagators(t *testing.T) {
	if _, err := Init(context.Background(), Config{ServiceName: "svc"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceIDFromHex(t, "4bf92f3577b34da6a3ce929d0e0e4736"),
		SpanID:     spanIDFromHex(t, "00f067aa0ba902b7"),
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	traceparent, ok := carrier["traceparent"]
	if !ok {
		t.Fatal("traceparent header was not injected, W3C tracecontext propagator not installed")
	}
	if !strings.Contains(traceparent, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Errorf("traceparent = %q, want it to contain the trace id", traceparent)
	}

	extractedCtx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
	extractedSC := trace.SpanContextFromContext(extractedCtx)
	if extractedSC.TraceID() != sc.TraceID() {
		t.Errorf("extracted trace id = %s, want %s", extractedSC.TraceID(), sc.TraceID())
	}
	if extractedSC.SpanID() != sc.SpanID() {
		t.Errorf("extracted span id = %s, want %s", extractedSC.SpanID(), sc.SpanID())
	}
}

func TestInit_WithEndpointBuildsExporterAndShutsDownCleanly(t *testing.T) {
	shutdown, err := Init(context.Background(), Config{
		ServiceName:    "svc",
		ServiceVersion: "1.0.0",
		Endpoint:       "http://127.0.0.1:4318",
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown func is nil")
	}

	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown() error = %v, want nil when no spans were recorded", err)
	}
}

func TestSamplerFromEnv(t *testing.T) {
	tests := []struct {
		name       string
		samplerEnv string
		argEnv     string
		wantDesc   string
	}{
		{name: "unset defaults to parent based always on", wantDesc: "ParentBased"},
		{name: "always_on", samplerEnv: "always_on", wantDesc: "AlwaysOnSampler"},
		{name: "always_off", samplerEnv: "always_off", wantDesc: "AlwaysOffSampler"},
		{name: "traceidratio", samplerEnv: "traceidratio", argEnv: "0.5", wantDesc: "TraceIDRatioBased{0.5}"},
		{name: "traceidratio invalid arg falls back to 1", samplerEnv: "traceidratio", argEnv: "bogus", wantDesc: "TraceIDRatioBased{1}"},
		{name: "parentbased_always_off", samplerEnv: "parentbased_always_off", wantDesc: "ParentBased"},
		{name: "parentbased_traceidratio", samplerEnv: "parentbased_traceidratio", argEnv: "0.25", wantDesc: "ParentBased"},
		{name: "unrecognized falls back to parent based always on", samplerEnv: "not-a-real-sampler", wantDesc: "ParentBased"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACES_SAMPLER", tt.samplerEnv)
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", tt.argEnv)

			sampler := samplerFromEnv()
			if got := sampler.Description(); !strings.Contains(got, tt.wantDesc) {
				t.Errorf("Description() = %q, want it to contain %q", got, tt.wantDesc)
			}
		})
	}
}

func traceIDFromHex(t *testing.T, s string) trace.TraceID {
	t.Helper()
	id, err := trace.TraceIDFromHex(s)
	if err != nil {
		t.Fatalf("TraceIDFromHex(%q) error = %v", s, err)
	}
	return id
}

func spanIDFromHex(t *testing.T, s string) trace.SpanID {
	t.Helper()
	id, err := trace.SpanIDFromHex(s)
	if err != nil {
		t.Fatalf("SpanIDFromHex(%q) error = %v", s, err)
	}
	return id
}
