# Build stage
FROM golang:1.25.13-alpine AS builder

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



# Runtime stage. The tag is pinned and nothing is installed here: an unpinned alpine plus
# `apk upgrade` rebuilt the image from whatever the mirror served that day, and made the build
# depend on reaching the alpine CDN. Certificates and timezone data come from the builder, and the
# health check uses the wget that busybox already provides.
FROM alpine:3.24

# Copy timezone data and certificates from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /app/main /main

# Expose port
ARG PORT
EXPOSE ${PORT}

# Set environment variable for the application
ENV PORT=${PORT}

# Health check. Shell form on purpose: the exec form does not expand ${PORT}.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider "http://localhost:${PORT}/health" || exit 1

# Run the binary
ENTRYPOINT ["/main"]
