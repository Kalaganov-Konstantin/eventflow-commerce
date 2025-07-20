# Build stage
FROM python:3.12-slim AS builder

# Install uv
RUN pip install uv

WORKDIR /app

# Copy dependency files
COPY pyproject.toml uv.lock ./

# Install dependencies
RUN uv sync --locked --no-dev

# Runtime stage
FROM python:3.12-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y     ca-certificates     && rm -rf /var/lib/apt/lists/*     && apt-get clean

WORKDIR /app

# Copy virtual environment from builder
COPY --from=builder /app/.venv /app/.venv

# Copy application code
COPY notification/ ./notification/

# Add virtual environment to PATH
ENV PATH="/app/.venv/bin:$PATH"

# Create non-root user
RUN groupadd -r appuser && useradd -r -g appuser appuser
RUN chown -R appuser:appuser /app
USER appuser

# Expose port
EXPOSE 8084

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3     CMD python -c "import requests; requests.get('http://localhost:8084/health')" || exit 1

# Run the application
CMD ["python", "-m", "notification.main"]
