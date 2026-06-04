import json
import logging
import os
import sys
from datetime import UTC, datetime

from opentelemetry import trace

SERVICE_NAME = "notification"
SERVICE_VERSION = "1.0.0"

_UVICORN_LOGGER_NAMES = ("uvicorn", "uvicorn.error", "uvicorn.access")


class JSONFormatter(logging.Formatter):
    """Renders a log record as a single line of json with service metadata."""

    def format(self, record: logging.LogRecord) -> str:
        payload = {
            "service": SERVICE_NAME,
            "version": SERVICE_VERSION,
            "level": record.levelname.lower(),
            "timestamp": datetime.fromtimestamp(record.created, tz=UTC).isoformat(),
            "message": record.getMessage(),
        }

        span_context = trace.get_current_span().get_span_context()
        if span_context.is_valid:
            payload["trace_id"] = format(span_context.trace_id, "032x")
            payload["span_id"] = format(span_context.span_id, "016x")

        correlation_id = getattr(record, "correlation_id", None)
        if correlation_id:
            payload["correlation_id"] = correlation_id

        if record.exc_info:
            payload["exception"] = self.formatException(record.exc_info)

        return json.dumps(payload)


def configure_logging() -> None:
    """Installs the json formatter on the root and uvicorn loggers outside development."""
    if os.getenv("ENVIRONMENT", "development") == "development":
        return

    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JSONFormatter())

    root = logging.getLogger()
    root.handlers = [handler]
    root.setLevel(logging.INFO)

    for name in _UVICORN_LOGGER_NAMES:
        uvicorn_logger = logging.getLogger(name)
        uvicorn_logger.handlers = [handler]
        uvicorn_logger.propagate = False
