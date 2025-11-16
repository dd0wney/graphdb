# Multi-stage Dockerfile for Cluso GraphDB
# Optimized for size and security

# Build stage
FROM golang:1.25-alpine AS builder

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
    -o /build/cluso-server \
    ./cmd/server

# Build the CLI binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /build/cluso-cli \
    ./cmd/cli

# Final stage - minimal runtime image
FROM alpine:latest

# Add non-root user
RUN addgroup -S cluso && adduser -S cluso -G cluso

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /build/cluso-server /app/
COPY --from=builder /build/cluso-cli /app/

# Create data directory with proper permissions
RUN mkdir -p /data && chown -R cluso:cluso /data /app

# Switch to non-root user
USER cluso

# Expose server port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default to running the server (PORT env var will be used if set)
CMD ["/app/cluso-server", "--data", "/data"]
