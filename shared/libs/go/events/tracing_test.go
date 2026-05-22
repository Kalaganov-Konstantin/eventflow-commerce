package events

import (
	"context"
	"testing"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func init() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	// A bare SDK provider still generates real trace and span ids on Start, with no exporter
	// needed, so startProducerSpan/startConsumerSpan produce valid ids to assert on below.
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
}

// TestTraceContext_RoundTripsThroughKafkaHeaders publishes a span context through
// injectTraceContext and confirms extractTraceContext recovers the same trace and span ids, so a
// consumer span opened from headers really is a child of the span that published the message.
func TestTraceContext_RoundTripsThroughKafkaHeaders(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("TraceIDFromHex() error = %v", err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("SpanIDFromHex() error = %v", err)
	}

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     false,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	var headers []kafka.Header
	injectTraceContext(ctx, &headers)

	if len(headers) == 0 {
		t.Fatal("injectTraceContext wrote no headers")
	}

	extractedCtx := extractTraceContext(context.Background(), headers)
	extractedSC := trace.SpanContextFromContext(extractedCtx)

	if extractedSC.TraceID() != traceID {
		t.Errorf("extracted trace id = %s, want %s", extractedSC.TraceID(), traceID)
	}
	if extractedSC.SpanID() != spanID {
		t.Errorf("extracted span id = %s, want %s", extractedSC.SpanID(), spanID)
	}
	if !extractedSC.IsSampled() {
		t.Error("extracted span context lost the sampled flag")
	}
}

func TestTraceContext_ExtractWithoutHeadersReturnsUnchangedContext(t *testing.T) {
	ctx := extractTraceContext(context.Background(), nil)
	if trace.SpanContextFromContext(ctx).IsValid() {
		t.Error("expected no valid span context when no trace headers are present")
	}
}

func TestStartProducerAndConsumerSpan_LinkAsParentAndChild(t *testing.T) {
	producerCtx, producerSpan := startProducerSpan(context.Background(), OrdersTopic)
	defer producerSpan.End()

	var headers []kafka.Header
	injectTraceContext(producerCtx, &headers)

	consumerCtx, consumerSpan := startConsumerSpan(context.Background(), OrdersTopic, headers)
	defer consumerSpan.End()

	producerTraceID := trace.SpanContextFromContext(producerCtx).TraceID()
	consumerTraceID := trace.SpanContextFromContext(consumerCtx).TraceID()

	if !producerTraceID.IsValid() {
		t.Fatal("producer span has no valid trace id; is a global TracerProvider installed for the test?")
	}
	if consumerTraceID != producerTraceID {
		t.Errorf("consumer trace id = %s, want %s (same trace as the producer)", consumerTraceID, producerTraceID)
	}
}
