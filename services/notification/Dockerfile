# Build stage
FROM python:3.12-slim AS builder

# Install uv
COPY --from=ghcr.io/astral-sh/uv:latest /uv /bin/uv

# Set working directory
WORKDIR /app

# Copy Python project files
COPY services/notification/pyproject.toml services/notification/uv.lock ./

# Install dependencies
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy
RUN uv sync --frozen --no-dev

# Copy source code
COPY services/notification/ ./

# Runtime stage
FROM python:3.12-slim

# Install curl for health checks
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

# Copy virtual environment from builder
COPY --from=builder /app/.venv /app/.venv

# Copy source code
COPY --from=builder /app/notification /app/notification
COPY --from=builder /app/migrations /app/migrations

# Set working directory
WORKDIR /app

# Add virtual environment to path
ENV PATH="/app/.venv/bin:$PATH"
ENV PYTHONPATH="/app/.venv/lib/python3.12/site-packages:$PYTHONPATH"

# Expose port
ARG PORT=8000
EXPOSE ${PORT}

# Set environment variable for the application
ENV PORT=${PORT}

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["curl", "-f", "http://localhost:${PORT}/health"]

# Run the application
CMD ["python", "-m", "notification.main"]
