# Docker Configuration

This directory contains Docker-related files for the Trading Engine.

## Files

- **Dockerfile** - Multi-stage Docker build for the Trading Engine service
- **init.sql** - PostgreSQL initialization script (if needed)

## Dockerfile Details

### Build Stage
- Base: `golang:1.21-alpine`
- Downloads Go dependencies
- Compiles the application with optimizations
- Creates standalone binary

### Runtime Stage
- Base: `alpine:latest`
- Minimal runtime environment (~5MB base + binary)
- Includes `curl` and `wget` for health checks
- Includes CA certificates for HTTPS connections

### Security Features
- Non-root user (implicit in Alpine)
- Multi-stage build (no build tools in runtime)
- Minimal attack surface
- Static binary (CGO_ENABLED=0)

## Building

### Using Docker Compose (Recommended)
```bash
docker compose up -d --build
```

### Manual Build
```bash
# Build image
docker build -t trading-engine:latest -f docker/Dockerfile .

# Run container
docker run -d \
  -p 8080:8080 \
  -p 8081:8081 \
  -p 9090:9090 \
  -e DB_HOST=postgres \
  -e REDIS_HOST=redis \
  --name trading-engine \
  trading-engine:latest
```

## Health Check

The Dockerfile doesn't include a HEALTHCHECK instruction because it's defined in `docker-compose.yml`:

```yaml
healthcheck:
  test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/healthz"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 40s
```

## Exposed Ports

- **8080** - HTTP API
- **8081** - WebSocket
- **9090** - Prometheus Metrics

## Environment Variables

Required:
- `DB_HOST` - PostgreSQL hostname
- `DB_PORT` - PostgreSQL port
- `DB_NAME` - Database name
- `DB_USER` - Database username
- `DB_PASSWORD` - Database password
- `REDIS_HOST` - Redis hostname
- `REDIS_PORT` - Redis port

Optional:
- `LOG_LEVEL` - Logging level (debug, info, warn, error)
- `HTTP_PORT` - API port (default: 8080)
- `WS_PORT` - WebSocket port (default: 8081)
- `METRICS_PORT` - Metrics port (default: 9090)

## Image Size

- Build stage: ~800MB (Go compiler + dependencies)
- Runtime image: ~20MB (Alpine + binary)

## Optimization Tips

### Faster Builds
```dockerfile
# Add .dockerignore to exclude unnecessary files
echo "*.md" >> .dockerignore
echo "load-test/" >> .dockerignore
echo "*.log" >> .dockerignore
```

### Smaller Images
```dockerfile
# Use specific Alpine version
FROM alpine:3.18

# Remove cache after apk install
RUN apk --no-cache add ca-certificates
```

### Better Caching
```dockerfile
# Copy go.mod and go.sum first (better layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code after
COPY . .
```

## Production Considerations

1. **Use specific versions**: Replace `golang:1.21-alpine` with `golang:1.21.5-alpine`
2. **Add labels**: For better image management
3. **Scan for vulnerabilities**: Use `docker scan trading-engine:latest`
4. **Sign images**: Use Docker Content Trust
5. **Use secrets**: Don't pass passwords via ENV in production

## Troubleshooting

### Build Fails
```bash
# Clean build cache
docker builder prune

# Build without cache
docker build --no-cache -t trading-engine:latest -f docker/Dockerfile .
```

### Container Exits Immediately
```bash
# Check logs
docker logs trading-engine

# Run interactively
docker run -it --rm trading-engine:latest sh
```

### Health Check Fails
```bash
# Test health endpoint manually
docker exec trading-engine wget -O- http://localhost:8080/healthz

# Check if service is listening
docker exec trading-engine netstat -tlnp
```

---

See [DOCKER_GUIDE.md](../DOCKER_GUIDE.md) for complete deployment instructions.
