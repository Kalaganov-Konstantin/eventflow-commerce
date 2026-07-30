# ADR-004: OTLP Instead of Jaeger Thrift

## Status
Accepted

## Context
`docs/architecture/observability.md` and the original `.env.example` assumed traces would reach
Jaeger over its native Thrift protocol, at `http://jaeger:14268/api/traces`, through
`JAEGER_ENDPOINT`. The OpenTelemetry Go SDK removed its Jaeger exporter; `otel-go` only ships OTLP
exporters now, so a Thrift-based integration would mean depending on an unmaintained exporter
package instead of the SDK's supported path.

## Decision
Every service exports traces as OTLP over HTTP instead. `shared/libs/go/tracing.Init` builds an
`otlptracehttp` exporter pointed at `Config.Endpoint`; the notification service does the same
through `opentelemetry-exporter-otlp-proto-http` (`notification/tracing.py`). Both read the same
environment variable, `OTEL_EXPORTER_OTLP_ENDPOINT`, set in `docker-compose.yml` to
`http://jaeger:4318`, Jaeger's built-in OTLP HTTP collector port (`COLLECTOR_OTLP_ENABLED=true` in
the `jaeger` service). `JAEGER_ENDPOINT` no longer exists anywhere in the codebase; the Go
services still expose it internally as `jaeger.endpoint` in their config structs, bound to the
`OTEL_EXPORTER_OTLP_ENDPOINT` variable, for lack of a reason to rename the config key.

An empty `OTEL_EXPORTER_OTLP_ENDPOINT` disables trace export rather than failing service startup;
the W3C `tracecontext` and `baggage` propagators are still installed either way, so trace context
still flows through HTTP headers and Kafka message headers
(`shared/libs/go/events/tracing.go`) even when nothing collects the spans.

## Consequences
### Positive
- Uses the OpenTelemetry SDK's supported export path instead of a removed, unmaintained exporter.
- Jaeger accepts OTLP natively, so no collector or protocol translator sits between the services
  and Jaeger.
- One environment variable, one protocol, shared by every service including the Python one,
  instead of a Go-specific Thrift endpoint and a separate scheme for notification.

### Negative
- Any dashboard, alert or runbook still written against the old `JAEGER_ENDPOINT` name or the
  Thrift port (`14268`) is stale and needs updating.
- Jaeger's OTLP ingestion is newer than its Thrift path; older Jaeger deployments that predate
  native OTLP support would need a separate OpenTelemetry Collector in front of them.
