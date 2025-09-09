# Multi-stage build for ext-proc gRPC server
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go.mod and go.sum for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o extproc-server \
    ./cmd/server

# Final stage
FROM alpine:latest

# Install runtime dependencies including grpc_health_probe
RUN apk --no-cache add ca-certificates wget && \
    wget -O /bin/grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.25/grpc_health_probe-linux-amd64 && \
    chmod +x /bin/grpc_health_probe

# Create non-root user
RUN addgroup -g 1000 -S extproc && \
    adduser -u 1000 -D -S -G extproc extproc

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/extproc-server /usr/local/bin/extproc-server
RUN chmod +x /usr/local/bin/extproc-server

# Create log directory
RUN mkdir -p /var/log/extproc && \
    chown -R extproc:extproc /var/log/extproc

# Switch to non-root user
USER extproc

# Expose gRPC port
EXPOSE 9001

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD grpc_health_probe -addr=:9001 || exit 1

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/extproc-server"]
