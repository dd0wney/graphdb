# Multi-stage Dockerfile for GraphDB
# Optimized for size and security

# Build stage. It runs on the build host and cross-compiles for the target
# platform: the image workflows build linux/amd64 and linux/arm64, and an
# emulated arm64 compile is slow.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

# Set by BuildKit (docker buildx, podman) for each target platform. A stage
# sees a predefined build argument only after it declares it. Only the legacy
# builder leaves them empty, and an empty GOOS or GOARCH compiles for the host.
ARG TARGETOS
ARG TARGETARCH

# Install build dependencies
RUN apk add --no-cache git make ca-certificates tzdata

WORKDIR /build

# Copy go mod files first (for better caching)
COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

# Copy source code
COPY . .

# The release workflows pass the image version. git describe cannot supply it:
# .dockerignore keeps .git out of the build context.
ARG VERSION=dev

# Build the server binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X main.Version=${VERSION}" \
    -o /build/graphdb-server \
    ./cmd/server

# Build the CLI binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
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
