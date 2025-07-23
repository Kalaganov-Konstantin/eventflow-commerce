# Use a single-stage build for reliability
FROM python:3.12-slim

# Install curl for health checks and uv for dependency management
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*
COPY --from=ghcr.io/astral-sh/uv:latest /uv /bin/uv

# Set working directory
WORKDIR /app

# Copy Python project files first to leverage Docker cache
COPY services/notification/pyproject.toml services/notification/uv.lock ./

# Install dependencies into the global site-packages using the --system flag.
# This is a more robust pattern in Docker than creating a venv.
RUN uv pip install --system .

# Copy the rest of the source code
COPY services/notification/ ./

# Expose port
ARG PORT=8000
EXPOSE ${PORT}

# Set environment variable for the application
ENV PORT=${PORT}

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["curl", "-f", "http://localhost:${PORT}/health"]

# Run the application using the global python
CMD ["python", "-m", "notification.main"]
