package events

import (
	"context"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies the tracer producer and consumer spans are recorded against.
const tracerName = "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"

var tracer = otel.Tracer(tracerName)

// kafkaHeaderCarrier adapts a *[]kafka.Header to propagation.TextMapCarrier, so a trace context
// can travel through Kafka message headers alongside the event payload.
type kafkaHeaderCarrier struct {
	headers *[]kafka.Header
}

func (c kafkaHeaderCarrier) Get(key string) string {
	for _, h := range *c.headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c kafkaHeaderCarrier) Set(key, value string) {
	for i, h := range *c.headers {
		if h.Key == key {
			(*c.headers)[i].Value = []byte(value)
			return
		}
	}
	*c.headers = append(*c.headers, kafka.Header{Key: key, Value: []byte(value)})
}

func (c kafkaHeaderCarrier) Keys() []string {
	keys := make([]string, len(*c.headers))
	for i, h := range *c.headers {
		keys[i] = h.Key
	}
	return keys
}

// injectTraceContext writes the span context carried by ctx into headers using the global
// propagator. It is a no-op when ctx carries no valid span context.
func injectTraceContext(ctx context.Context, headers *[]kafka.Header) {
	otel.GetTextMapPropagator().Inject(ctx, kafkaHeaderCarrier{headers: headers})
}

// extractTraceContext returns ctx augmented with the span context encoded in headers, if any, so
// it can be used as the parent of a new span.
func extractTraceContext(ctx context.Context, headers []kafka.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, kafkaHeaderCarrier{headers: &headers})
}

// startProducerSpan opens a producer span for a publish to topic. The returned context carries
// the span and should be used both for the write and for injecting trace headers into the
// outgoing message.
func startProducerSpan(ctx context.Context, topic string) (context.Context, trace.Span) {
	return tracer.Start(ctx, topic+" publish", trace.WithSpanKind(trace.SpanKindProducer))
}

// startConsumerSpan extracts any trace context carried by headers and opens a consumer span as
// its child, so the consumer side of an event shows up nested under the span that published it.
func startConsumerSpan(ctx context.Context, topic string, headers []kafka.Header) (context.Context, trace.Span) {
	parentCtx := extractTraceContext(ctx, headers)
	return tracer.Start(parentCtx, topic+" process", trace.WithSpanKind(trace.SpanKindConsumer))
}
