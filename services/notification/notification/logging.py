import json
import logging
import os
import sys
from datetime import UTC, datetime

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
