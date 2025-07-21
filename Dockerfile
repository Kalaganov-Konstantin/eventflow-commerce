# Build stage
FROM golang:1.24.5-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

ARG SERVICE_NAME
WORKDIR /app

# Copy entire project (filtered by .dockerignore)
COPY . .

# Build the specified service
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o main ./services/${SERVICE_NAME}/cmd/server



# Runtime stage
FROM alpine:latest

# Install curl for health checks and update package index
RUN apk update && apk upgrade && apk add --no-cache curl ca-certificates tzdata

# Copy timezone data and certificates from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /app/main /main

# Expose port
ARG PORT
EXPOSE ${PORT}

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD [ "curl", "-f", "http://localhost:${PORT}/health" ]

# Set environment variable for the application
ENV PORT=${PORT}

# Run the binary
ENTRYPOINT ["/main"]
