# Multi-stage Dockerfile for GraphDB
# Optimized for size and security

# Build stage
FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make ca-certificates tzdata

WORKDIR /build

# Copy go mod files first (for better caching)
COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

# Copy source code
COPY . .

# Build the server binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.Version=$(git describe --tags --always --dirty)" \
    -o /build/graphdb-server \
    ./cmd/server

# Build the CLI binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /build/graphdb-cli \
    ./cmd/cli

# Final stage - minimal runtime image
FROM alpine:latest

# Add non-root user with a fixed numeric uid/gid so Kubernetes runAsNonRoot works
RUN addgroup -S -g 10001 graphdb && adduser -S -u 10001 -G graphdb graphdb

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /build/graphdb-server /app/
COPY --from=builder /build/graphdb-cli /app/

# Create data directory with proper permissions
RUN mkdir -p /data && chown -R graphdb:graphdb /data /app

# Switch to non-root user
USER graphdb

# Expose server port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default to running the server (PORT env var will be used if set)
CMD ["/app/graphdb-server", "--data", "/data"]
