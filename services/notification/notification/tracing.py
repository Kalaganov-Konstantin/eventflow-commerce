from fastapi import FastAPI
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.sdk.resources import SERVICE_NAME, SERVICE_VERSION, Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

SERVICE_NAME_VALUE = "notification"
SERVICE_VERSION_VALUE = "1.0.0"


def setup_tracing(app: FastAPI, otlp_endpoint: str) -> None:
    """Install an OTLP tracer provider and instrument app, a no-op when otlp_endpoint is empty."""
    if not otlp_endpoint:
        return

    resource = Resource.create(
        {SERVICE_NAME: SERVICE_NAME_VALUE, SERVICE_VERSION: SERVICE_VERSION_VALUE}
    )
    provider = TracerProvider(resource=resource)
    exporter = OTLPSpanExporter(endpoint=f"{otlp_endpoint.rstrip('/')}/v1/traces")
    provider.add_span_processor(BatchSpanProcessor(exporter))
    trace.set_tracer_provider(provider)

    FastAPIInstrumentor.instrument_app(app)
