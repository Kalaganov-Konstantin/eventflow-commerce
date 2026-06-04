import io
import json
import logging

from opentelemetry import context as otel_context
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider

from notification.logging import _UVICORN_LOGGER_NAMES, JSONFormatter, configure_logging


def _log_line(logger: logging.Logger, message: str) -> dict:
    stream = io.StringIO()
    handler = logging.StreamHandler(stream)
    handler.setFormatter(JSONFormatter())
    logger.addHandler(handler)
    logger.setLevel(logging.INFO)
    try:
        logger.info(message)
    finally:
        logger.removeHandler(handler)
    return json.loads(stream.getvalue().strip())


def test_json_formatter_includes_required_fields():
    logger = logging.getLogger("test.logging.formatter")

    payload = _log_line(logger, "order created")

    assert payload["service"] == "notification"
    assert payload["version"] == "1.0.0"
    assert payload["level"] == "info"
    assert payload["message"] == "order created"
    assert "timestamp" in payload


def test_json_formatter_emits_one_line_per_record():
    logger = logging.getLogger("test.logging.formatter.multi")
    stream = io.StringIO()
    handler = logging.StreamHandler(stream)
    handler.setFormatter(JSONFormatter())
    logger.addHandler(handler)
    logger.setLevel(logging.INFO)
    try:
        logger.info("first")
        logger.info("second")
    finally:
        logger.removeHandler(handler)

    lines = stream.getvalue().strip().splitlines()
    assert len(lines) == 2
    assert [json.loads(line)["message"] for line in lines] == ["first", "second"]


def test_json_formatter_includes_exception_details():
    logger = logging.getLogger("test.logging.formatter.exception")
    stream = io.StringIO()
    handler = logging.StreamHandler(stream)
    handler.setFormatter(JSONFormatter())
    logger.addHandler(handler)
    logger.setLevel(logging.INFO)
    try:
        try:
            raise ValueError("boom")
        except ValueError:
            logger.exception("failed to process")
    finally:
        logger.removeHandler(handler)

    payload = json.loads(stream.getvalue().strip())
    assert "ValueError: boom" in payload["exception"]


def test_json_formatter_includes_trace_and_span_ids_from_active_span():
    logger = logging.getLogger("test.logging.formatter.trace")
    tracer = TracerProvider().get_tracer(__name__)
    span = tracer.start_span("test-span")
    token = otel_context.attach(trace.set_span_in_context(span))
    try:
        payload = _log_line(logger, "traced")
    finally:
        otel_context.detach(token)
        span.end()

    span_context = span.get_span_context()
    assert payload["trace_id"] == format(span_context.trace_id, "032x")
    assert payload["span_id"] == format(span_context.span_id, "016x")


def test_json_formatter_omits_trace_and_span_ids_without_active_span():
    logger = logging.getLogger("test.logging.formatter.no_trace")

    payload = _log_line(logger, "untraced")

    assert "trace_id" not in payload
    assert "span_id" not in payload


def test_json_formatter_includes_correlation_id_when_provided():
    logger = logging.getLogger("test.logging.formatter.correlation")
    stream = io.StringIO()
    handler = logging.StreamHandler(stream)
    handler.setFormatter(JSONFormatter())
    logger.addHandler(handler)
    logger.setLevel(logging.INFO)
    try:
        logger.info("with correlation", extra={"correlation_id": "corr-1"})
    finally:
        logger.removeHandler(handler)

    payload = json.loads(stream.getvalue().strip())
    assert payload["correlation_id"] == "corr-1"


def test_json_formatter_omits_correlation_id_when_absent():
    logger = logging.getLogger("test.logging.formatter.no_correlation")

    payload = _log_line(logger, "no correlation")

    assert "correlation_id" not in payload


def test_configure_logging_noop_in_development(monkeypatch):
    monkeypatch.setenv("ENVIRONMENT", "development")
    root = logging.getLogger()
    original_handlers = list(root.handlers)

    configure_logging()

    assert root.handlers == original_handlers


def test_configure_logging_installs_json_formatter_outside_development(monkeypatch):
    monkeypatch.setenv("ENVIRONMENT", "production")
    root = logging.getLogger()
    original_handlers = list(root.handlers)
    original_level = root.level

    try:
        configure_logging()

        assert len(root.handlers) == 1
        assert isinstance(root.handlers[0].formatter, JSONFormatter)

        for name in _UVICORN_LOGGER_NAMES:
            uvicorn_logger = logging.getLogger(name)
            assert uvicorn_logger.propagate is False
            assert len(uvicorn_logger.handlers) == 1
            assert isinstance(uvicorn_logger.handlers[0].formatter, JSONFormatter)
    finally:
        root.handlers = original_handlers
        root.setLevel(original_level)
        for name in _UVICORN_LOGGER_NAMES:
            logging.getLogger(name).handlers = []
            logging.getLogger(name).propagate = True
