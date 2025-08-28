# Traefik ext-proc Plugin POC

## Overview

This POC demonstrates a functional ext-proc based plugin system for Traefik that can replace Yaegi plugins. The implementation showcases header manipulation through external gRPC processing, providing a foundation for the future plugin architecture.

**Core Functionality:**
- Extract `X-Request-Header` from incoming HTTP requests
- Process through external gRPC server using Envoy's ext-proc protocol
- Add `X-Response-Header` to HTTP responses  
- Maintain Traefik middleware chain execution

**Architecture:**
```
HTTP Request → Traefik → ext-proc Middleware → gRPC Server → Response Processing
```

## Quick Start

### Prerequisites
- Docker and Docker Compose
- Make (for development commands)
- Go 1.24+ (for local development)

### Running the POC

```bash
# Start the complete environment
make up

# Check service status
make status  

# Test the functionality
make test

# View logs
make logs

# Stop the environment
make down
```

### Testing Header Processing

```bash
# Test with X-Request-Header (should add X-Response-Header)
curl -H "X-Request-Header: test-value" -H "Host: whoami.localhost" -I http://localhost/

# Test without X-Request-Header (should work normally)  
curl -H "Host: whoami.localhost" http://localhost/
```

## Architecture

### Components

1. **Traefik with ext-proc Middleware**: Custom Traefik build including the ext-proc middleware
2. **ext-proc gRPC Server**: External processing server implementing Envoy's ext-proc protocol  
3. **Test Application**: whoami service for testing the complete flow

### Request Flow

1. HTTP request arrives at Traefik
2. ext-proc middleware intercepts the request
3. Request headers sent to gRPC server via ProcessingRequest
4. Server extracts X-Request-Header value
5. Processing continues through middleware chain
6. Response headers sent to gRPC server  
7. Server adds X-Response-Header with extracted value
8. Response returned to client

## Development

### Available Commands

```bash
make help                    # Show all available commands
make build                   # Build all Docker services
make up                      # Start the POC environment  
make down                    # Stop the POC environment
make status                  # Check service health
make test                    # Run functional tests
make logs                    # View all service logs
make logs-traefik           # View Traefik logs only
make logs-extproc           # View ext-proc server logs only
make clean                   # Complete cleanup
make traefik-restart        # Restart Traefik only
make extproc-restart        # Restart ext-proc server only
```

### Development Workflow

1. Make changes to Traefik middleware code
2. `make traefik-restart` to rebuild and restart Traefik
3. `make test` to validate changes
4. `make logs-traefik` to debug issues

For ext-proc server changes:
1. Modify server code in `extproc-server/`
2. `make extproc-restart` to rebuild and restart
3. `make test` to validate

## Configuration

### Traefik Configuration

Static configuration (`config/traefik.yml`):
- API dashboard enabled on port 8080
- Docker provider for service discovery
- File provider for dynamic configuration

Dynamic configuration (`config/dynamic.yml`):
- ext-proc middleware definition
- Routing rules for test services

### ext-proc Server Configuration

The server accepts the following environment variables:
- `GRPC_PORT`: gRPC server port (default: 9001)
- `LOG_LEVEL`: Logging level (default: DEBUG)  
- `TIMEOUT`: Processing timeout (default: 5s)

## Project Structure

```
poc-ext-proc-plugin/
├── README.md                    # This file
├── plan.md                      # Detailed development plan
├── Makefile                     # Development commands
├── docker-compose.yml           # Service orchestration
├── docker-compose.override.yml  # Development overrides
├── config/                      # Traefik configuration
│   ├── Dockerfile.traefik       # Custom Traefik build
│   ├── traefik.yml              # Static configuration
│   └── dynamic.yml              # Dynamic configuration  
├── extproc-server/              # gRPC server implementation
│   ├── go.mod                   # Go module
│   ├── Dockerfile               # Server container
│   ├── cmd/server/              # Server entry point
│   └── pkg/extproc/             # Server implementation
├── tests/                       # Test files and scripts
└── logs/                        # Application logs
```

## Testing

### Manual Testing

```bash
# Test with header present
curl -H "X-Request-Header: hello-world" -H "Host: whoami.localhost" -v http://localhost/

# Test without header
curl -H "Host: whoami.localhost" -v http://localhost/

# Check response headers
curl -H "X-Request-Header: test" -H "Host: whoami.localhost" -I http://localhost/ | grep -i x-response
```

### Automated Testing

```bash
make test                # Functional tests
make test-stress         # Load testing with wrk
```

## Troubleshooting

### Common Issues

**Services not starting:**
```bash
make status              # Check service health
make logs               # Check for errors
```

**ext-proc server connection issues:**
```bash
# Check gRPC health
docker-compose exec extproc-plugin grpc_health_probe -addr=:9001

# Check network connectivity  
docker-compose exec traefik ping extproc-plugin
```

**Headers not being processed:**
```bash
# Check middleware configuration
curl -s http://localhost:8080/api/rawdata | jq '.http.middlewares'

# Check ext-proc server logs
make logs-extproc
```

### Debug Mode

Enable additional debugging:
```bash
# Edit .env file
TRAEFIK_LOG_LEVEL=DEBUG
EXTPROC_LOG_LEVEL=DEBUG

# Restart services
make down && make up
```

## Performance

### Expected Performance Characteristics

- **Added Latency**: < 10ms p95 for header processing
- **Throughput**: Minimal impact on request throughput  
- **Resource Usage**: Low CPU and memory overhead

### Performance Testing

```bash
# Run stress test with monitoring
make test-stress

# Monitor resource usage
docker stats
```

## Limitations

### Current POC Limitations

- **Header Processing Only**: Only request/response headers supported (no body processing)
- **Single Server**: No load balancing of ext-proc servers
- **Basic Error Handling**: Simplified fallback mechanisms
- **Insecure gRPC**: TLS not configured for development simplicity

### Production Considerations

- Enable gRPC TLS for secure communication
- Implement proper ext-proc server load balancing
- Add comprehensive monitoring and metrics
- Implement advanced error handling and circuit breakers

## Next Steps

1. **Performance Optimization**: Implement connection pooling and caching
2. **Security Enhancement**: Add TLS and authentication
3. **Feature Expansion**: Add request/response body processing
4. **Production Readiness**: Add monitoring, metrics, and alerting
5. **Migration Tools**: Create utilities for migrating from Yaegi plugins

## Contributing

This is a POC implementation. For production use, additional development and testing would be required.

### Development Environment Setup

```bash
# Clone the repository (if standalone)
git clone <repository-url>
cd poc-ext-proc-plugin

# Start development environment
make dev

# Run tests
make test
```

## License

This POC follows the same license as the main Traefik project (MIT License).