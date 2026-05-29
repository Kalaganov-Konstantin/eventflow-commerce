from fastapi import FastAPI
from opentelemetry import propagate, trace
from opentelemetry.sdk.trace import TracerProvider

from notification.consumer import _extract_trace_context
from notification.tracing import setup_tracing


def test_setup_tracing_noop_when_endpoint_empty():
    app = FastAPI()

    setup_tracing(app, "")

    assert getattr(app, "_is_instrumented_by_opentelemetry", False) is False


def test_setup_tracing_instruments_app_when_endpoint_set():
    app = FastAPI()

    setup_tracing(app, "http://localhost:4318")

    assert app._is_instrumented_by_opentelemetry is True


def test_extract_trace_context_round_trips_through_kafka_headers():
    provider = TracerProvider()
    tracer = provider.get_tracer(__name__)

    span = tracer.start_span("producer")
    carrier: dict[str, str] = {}
    propagate.inject(carrier, context=trace.set_span_in_context(span))
    expected_trace_id = span.get_span_context().trace_id
    span.end()

    headers = [(key, value.encode()) for key, value in carrier.items()]

    ctx = _extract_trace_context(headers)
    extracted = trace.get_current_span(ctx).get_span_context()

    assert extracted.is_valid
    assert extracted.trace_id == expected_trace_id


def test_extract_trace_context_without_headers_returns_no_span():
    ctx = _extract_trace_context([])

    assert trace.get_current_span(ctx).get_span_context().is_valid is False
