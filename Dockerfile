# ========================================================
# Target 1: development
# Includes full Go toolchain, testing utilities, and source mounts.
# ========================================================
FROM golang:1.24-alpine AS development

# Set environment overrides
ENV CGO_ENABLED=0 \
    GOOS=linux

WORKDIR /app

# Install development dependencies (git, build-base, pgrep)
RUN apk add --no-cache git build-base procps

# Copy dependencies manifest
COPY go.mod ./
RUN go mod download

# Copy source code
COPY . .

# Run test suite by default in development target
CMD ["go", "test", "-v", "./..."]


# ========================================================
# Target 2: builder (intermediate for production compilation)
# ========================================================
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

# Build fully static optimized binary (-s -w strips debug symbols and DWARF tables)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -extldflags '-static'" \
    -o /app/queuectl ./cmd/queuectl/main.go


# ========================================================
# Target 3: production (final minimalist image)
# Uses clean Alpine, runs under a non-root user, and enforces health checks.
# ========================================================
FROM alpine:3.20 AS production

# Add security CA certificates and timezone definitions
RUN apk --no-cache add ca-certificates tzdata

# Create dedicated non-root execution group and user
RUN addgroup -g 10001 -S queuectl && \
    adduser -u 10001 -S -G queuectl queuectl

# Pre-create data and configuration directory with non-root ownership
RUN mkdir -p /data /etc/queuectl && \
    chown -R queuectl:queuectl /data /etc/queuectl

# Import statically compiled binary from builder stage
COPY --from=builder --chown=queuectl:queuectl /app/queuectl /usr/local/bin/queuectl
COPY --from=builder --chown=queuectl:queuectl /app/config.yaml /etc/queuectl/config.yaml

# Set runtime container directories
WORKDIR /data
VOLUME ["/data"]

# Enforce secure non-root user execution
USER 10001:10001

# Production Docker Healthcheck (utilizes metrics telemetry queries)
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["queuectl", "metrics", "--config", "/etc/queuectl/config.yaml"]

# Default entry point
ENTRYPOINT ["queuectl"]
CMD ["--help"]
