# Contributing to Trading Engine

Thank you for your interest in contributing! This document provides guidelines and workflows for developing the trading engine.

## 🌳 Branch Workflow

We follow a **Git Flow** inspired workflow:

### Branch Types

- **`main`** - Production-ready code, always stable
- **`develop`** - Integration branch for features
- **`feature/*`** - New features (e.g., `feature/websocket-broadcast`)
- **`fix/*`** - Bug fixes (e.g., `fix/order-matching-race`)
- **`refactor/*`** - Code refactoring (e.g., `refactor/persistence-layer`)
- **`test/*`** - Test improvements (e.g., `test/load-testing`)
- **`docs/*`** - Documentation (e.g., `docs/api-examples`)

### Workflow

```bash
# 1. Clone and setup
git clone <repository-url>
cd trading-engine
git checkout develop

# 2. Create feature branch
git checkout -b feature/order-cancellation

# 3. Make changes and commit frequently
git add .
git commit -m "feat(api): add order cancellation endpoint"

# 4. Keep branch updated
git fetch origin
git rebase origin/develop

# 5. Push and create PR
git push origin feature/order-cancellation
# Open PR: feature/order-cancellation -> develop
```

## 📝 Commit Message Convention

We use **Conventional Commits** format:

```
<type>(<scope>): <subject>

<body>

<footer>
```

### Types
- **feat**: New feature
- **fix**: Bug fix
- **refactor**: Code refactoring
- **test**: Adding or updating tests
- **docs**: Documentation changes
- **perf**: Performance improvements
- **chore**: Build process, dependencies, etc.

### Examples
```bash
feat(engine): implement price-time priority matching
fix(api): prevent duplicate order submission race condition
test(engine): add concurrent order matching tests
docs(readme): update API examples with curl commands
refactor(persistence): extract database layer interface
perf(engine): optimize orderbook lookup with binary search
```

## 🛠️ Development Setup

### Prerequisites
```bash
# Install Go 1.21+
go version

# Install Docker & Docker Compose
docker --version
docker-compose --version

# Install Make (optional)
make --version

# Install development tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### First Time Setup
```bash
# 1. Start dependencies
docker-compose up -d postgres redis

# 2. Copy environment variables
cp .env.example .env

# 3. Install Go dependencies
go mod download

# 4. Verify setup
make test
```

## Project Structure

```
trading-engine/
├── cmd/
│   └── server/
│       └── main.go           # Application entry point
├── internal/                  # Private application code
│   ├── engine/
│   │   ├── matcher.go        # Core matching logic
│   │   ├── orderbook.go      # In-memory order book
│   │   └── engine_test.go
│   ├── api/
│   │   ├── handlers.go       # HTTP handlers
│   │   ├── middleware.go     # Auth, rate limiting
│   │   └── router.go
│   ├── websocket/
│   │   ├── hub.go            # WebSocket connection manager
│   │   └── broadcaster.go
│   └── metrics/
│       └── prometheus.go     # Metrics collection
├── pkg/                       # Public, reusable packages
│   ├── logger/
│   └── validator/
├── models/                    # Data models
│   ├── order.go
│   ├── trade.go
│   └── orderbook.go
├── persistence/               # Database layer
│   ├── postgres.go
│   ├── redis.go
│   └── repository.go
├── config/
│   ├── config.go             # Config loading
│   └── config.yaml
├── fixtures/
│   └── gen_orders.go         # Test data generator
├── load-test/
│   └── load-test.js          # k6 load test
└── tests/
    ├── integration_test.go
    └── helpers.go
```

## 🧪 Testing Guidelines

### Unit Tests
```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Test specific package
go test ./internal/engine -v
```

### Integration Tests
```bash
# Ensure services are running
docker-compose up -d postgres redis

# Run integration tests
go test -tags=integration ./tests/...
```

### Writing Tests
```go
// Example unit test
func TestMatchingEngine_LimitOrder(t *testing.T) {
    engine := NewMatchingEngine()
    
    order := &models.Order{
        OrderID:   "order-1",
        Side:      "buy",
        Price:     100.0,
        Quantity:  10.0,
    }
    
    err := engine.ProcessOrder(order)
    assert.NoError(t, err)
    assert.Equal(t, "open", order.Status)
}
```

## 🔍 Code Quality

### Linting
```bash
# Run linter
golangci-lint run

# Auto-fix issues
golangci-lint run --fix
```

### Code Style
- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` for formatting (runs automatically)
- Keep functions small and focused (<50 lines)
- Write clear variable names (no single letters except loop counters)
- Add comments for exported functions and complex logic

### Pre-commit Checklist
- [ ] Tests pass: `make test`
- [ ] Linter passes: `make lint`
- [ ] Code formatted: `gofmt -w .`
- [ ] No debug logs or commented code
- [ ] Updated documentation if needed

## Running Locally

### Full Stack (Docker)
```bash
# Start everything
docker-compose up --build

# View logs
docker-compose logs -f trading-engine

# Stop everything
docker-compose down
```

### Local Development
```bash
# Start dependencies only
docker-compose up -d postgres redis

# Run application
go run cmd/server/main.go

# OR with hot reload (install air first)
air

# In another terminal, test endpoints
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "test-1",
    "client_id": "client-A",
    "instrument": "BTC-USD",
    "side": "buy",
    "type": "limit",
    "price": 70000,
    "quantity": 1.0
  }'
```

### Database Management
```bash
# Access Postgres
docker-compose exec postgres psql -U trader -d trading

# Run migrations
docker-compose exec postgres psql -U trader -d trading -f /docker-entrypoint-initdb.d/init.sql

# Reset database
docker-compose down -v
docker-compose up -d postgres
```

### Redis Management
```bash
# Access Redis CLI
docker-compose exec redis redis-cli

# View streams
XINFO STREAM order_events

# Clear Redis
FLUSHALL
```

## >Performance Testing

### Load Testing with k6
```bash
# Generate fixtures
cd fixtures
go run gen_orders.go

# Run load test
cd ../load-test
k6 run load-test.js

# Run with custom parameters
k6 run --vus 100 --duration 60s load-test.js
```

### Profiling
```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=.
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=.
go tool pprof mem.prof
```

## 🐛 Debugging

### Debug Logging
```bash
# Enable debug logs
export LOG_LEVEL=debug
go run cmd/server/main.go
```

### Delve Debugger
```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug
dlv debug cmd/server/main.go
```

## 📦 Creating a Release

```bash
# 1. Ensure you're on develop
git checkout develop
git pull origin develop

# 2. Create release branch
git checkout -b release/v1.0.0

# 3. Update version numbers
# Edit version in code, README, etc.

# 4. Commit
git commit -am "chore(release): bump version to 1.0.0"

# 5. Merge to main
git checkout main
git merge release/v1.0.0
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin main --tags

# 6. Merge back to develop
git checkout develop
git merge main
git push origin develop
```

## Feature Development Checklist

When adding a new feature:

- [ ] Create feature branch from `develop`
- [ ] Write tests first (TDD approach)
- [ ] Implement feature
- [ ] Add unit tests (>80% coverage)
- [ ] Add integration tests if needed
- [ ] Update API documentation
- [ ] Add metrics/logging
- [ ] Update README if user-facing
- [ ] Run full test suite
- [ ] Create PR with clear description
- [ ] Request code review

## Pull Request Process

1. **Update your branch** with latest `develop`
2. **Run all tests** and ensure they pass
3. **Write clear PR description**:
   - What: Brief summary of changes
   - Why: Reason for the change
   - How: Technical approach
   - Testing: How you tested it
4. **Request reviewers**
5. **Address feedback** promptly
6. **Squash commits** if needed before merge

## ❓ Questions?

- Open an issue for bugs or feature requests
- Tag maintainers for urgent issues
- Check existing issues before creating new ones

## Resources

- [Effective Go](https://golang.org/doc/effective_go.html)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Conventional Commits](https://www.conventionalcommits.org/)
- [Git Flow](https://nvie.com/posts/a-successful-git-branching-model/)

Thank you for contributing! 🎉
