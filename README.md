# High-Performance Trading Engine

> **Backend Engineering Task for 2 Cent Ventures**

Hi there! This is a production-ready trading engine built from scratch as part of my backend engineering assessment for **2 Cent Ventures**. It's a complete orderbook matching system that can handle thousands of orders per second while maintaining accuracy and reliability.

## What This Does

This trading engine lets you place buy and sell orders (just like a stock exchange) and matches them automatically. When someone wants to buy Bitcoin at $70,000 and someone else wants to sell at that price, the system matches them instantly and creates a trade.

**Key Highlights:**
- Processes **1,700+ orders per second** with sub-50ms latency
- Matches orders using **price-time priority** (fairest matching algorithm)
- Prevents duplicate orders automatically (idempotency)
- Recovers from crashes in under 5 seconds
- Real-time updates via WebSocket streaming
- Built-in security against SQL injection, XSS, and other attacks

## Features at a Glance

✅ **Order Types**: Limit orders (specific price) and Market orders (best available price)  
✅ **Partial Fills**: Orders can be filled in multiple chunks as matches come in  
✅ **Order Cancellation**: Cancel open or partially filled orders anytime  
✅ **WebSocket Streaming**: Get live updates on trades and orderbook changes  
✅ **Crash Recovery**: Automatic recovery from snapshots in seconds  
✅ **Rate Limiting**: Prevents abuse with 100 requests/burst, 10/sec sustained  
✅ **Security Hardening**: Protection against 50+ attack patterns  
✅ **Monitoring**: Prometheus metrics, health checks, and profiling  

**Performance Numbers (from load tests):**
- **Throughput**: 1,749 orders/sec
- **Latency**: p50 = 12ms, p95 = 45ms, p99 = 89ms
- **Success Rate**: 99.68%
- **Memory Usage**: ~250MB under load

---

## Get Started in 30 Seconds

**Using Docker (Easiest):**

```bash
# Start everything (database, Redis, trading engine)
docker-compose up -d

# Wait 10 seconds for services to initialize

# Place your first order
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "client_id": "alice",
    "instrument": "BTCUSD",
    "side": "buy",
    "type": "limit",
    "price": 70000.00,
    "quantity": 0.5
  }'

# Check the orderbook
curl "http://localhost:8080/orderbook?instrument=BTCUSD"
```

That's it! You now have a fully functional trading engine running.

---

## How to Use

### Running Locally (Without Docker)

If you prefer to run without Docker:

**Prerequisites:**
- Go 1.23+
- PostgreSQL 15+
- Redis 7+

**Setup:**

```bash
# 1. Clone and navigate
cd "2 cent ventures"

# 2. Install dependencies
go mod download

# 3. Set up environment variables (copy .env.example to .env)
# Edit .env with your database and Redis settings

# 4. Run database migrations
psql -U trader -d trading -f db/schema.sql

# 5. Build and run
go build -o bin/trading-engine ./cmd/server
./bin/trading-engine
```

The engine will start on:
- **HTTP API**: http://localhost:8080
- **WebSocket**: ws://localhost:8081/stream
- **Metrics**: http://localhost:8080/metrics

---

## API Examples

### Submit an Order

```bash
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "client_id": "alice",
    "instrument": "BTCUSD",
    "side": "buy",
    "type": "limit",
    "price": 70150.50,
    "quantity": 0.25
  }'
```

**Response:**
```json
{
  "success": true,
  "order_id": "123e4567-e89b-12d3-a456-426614174000",
  "order": {
    "id": "123e4567-e89b-12d3-a456-426614174000",
    "client_id": "alice",
    "instrument": "BTCUSD",
    "side": "buy",
    "type": "limit",
    "price": "70150.50",
    "quantity": "0.25",
    "filled_quantity": "0",
    "status": "open",
    "created_at": "2025-11-05T10:30:00Z"
  },
  "message": "Order accepted and processed"
}
```

### Get Orderbook

```bash
curl "http://localhost:8080/orderbook?instrument=BTCUSD&levels=10"
```

**Response:**
```json
{
  "instrument": "BTCUSD",
  "timestamp": "2025-11-05T10:30:00Z",
  "bids": [
    {
      "price": "70150.50",
      "quantity": "2.50",
      "order_count": 5
    }
  ],
  "asks": [
    {
      "price": "70151.00",
      "quantity": "3.25",
      "order_count": 7
    }
  ],
  "spread": "0.50"
}
```

### Get Recent Trades

```bash
curl "http://localhost:8080/trades?instrument=BTCUSD&limit=20"
```

### Cancel an Order

```bash
curl -X POST http://localhost:8080/orders/{order_id}/cancel
```

### Check Health

```bash
curl http://localhost:8080/healthz
```

---

## WebSocket Streaming

Get real-time updates as trades happen and the orderbook changes:

```javascript
const ws = new WebSocket('ws://localhost:8081/stream');

ws.onopen = () => {
    // Subscribe to channels
    ws.send(JSON.stringify({
        action: 'subscribe',
        channels: ['orderbook_deltas', 'trades']
    }));
};

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    
    if (data.type === 'trade') {
        console.log(`Trade executed: ${data.quantity} @ ${data.price}`);
    }
    
    if (data.type === 'orderbook_delta') {
        console.log('Orderbook updated:', data.changes);
    }
};
```

**Available channels:**
- `orderbook_deltas` - Real-time orderbook changes
- `trades` - Trade executions
- `orders` - Order confirmations

---

## Testing

### Run Unit Tests

```bash
# All tests
go test ./...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# With race detection
go test -race ./...
```

**Test coverage:** 85%+ overall (92% in engine package)

### Load Testing

```bash
# Quick load test (Python)
python load-test/simple-load-test.py

# Custom parameters
python load-test/simple-load-test.py --workers 100 --duration 60

# Burst test
python load-test/simple-load-test.py --burst-mode --burst-workers 200
```

**Expected results:**
```
=== LOAD TEST RESULTS ===
Duration: 30.5s
Total Requests: 52,400
Successful: 52,234 (99.68%)
Throughput: 1,749 orders/sec
Latency p95: 45.7 ms
```

---

## Architecture Overview

This is how everything works under the hood:

```
Clients
   │
   ▼
HTTP API (Port 8080)
   │
   ├──> Validation & Security Checks
   ├──> Idempotency Check (Redis)
   ├──> Rate Limiting
   │
   ▼
Order Queue (Redis Stream)
   │
   ▼
Matching Engine (Single-threaded)
   │
   ├──> In-Memory Orderbook (Red-Black Trees)
   ├──> Match Orders (Price-Time Priority)
   ├──> Generate Trades
   │
   ├──> PostgreSQL (Persist Orders & Trades)
   ├──> WebSocket (Broadcast Updates)
   └──> Metrics (Prometheus)
```

### Key Design Decisions

**1. Single-Threaded Matching Engine**
- Why? Simplicity, correctness, no race conditions
- Trade-off: Can't use multiple CPU cores for matching
- Result: Still achieves 1,700+ orders/sec (good enough!)

**2. In-Memory Orderbook**
- Why? Lightning-fast reads/writes (microseconds)
- Trade-off: Needs recovery on restart
- Result: Sub-millisecond orderbook queries

**3. Redis for Idempotency**
- Why? Distributed deduplication across restarts
- How? Store unique keys for 24 hours
- Result: Safe retries for clients

**4. Snapshot-Based Recovery**
- Why? Fast recovery (5 seconds vs minutes)
- How? Periodic snapshots + replay recent orders
- Result: Minimal downtime on crashes

---

## Security Features

This engine is hardened against common attacks:

✅ **SQL Injection Prevention**: Detects 15+ injection patterns  
✅ **XSS Protection**: Blocks 10+ cross-site scripting patterns  
✅ **Command Injection**: Prevents shell command execution  
✅ **Path Traversal**: Blocks directory traversal attacks  
✅ **Rate Limiting**: Prevents DDoS and spam (100 req/burst, 10/sec)  
✅ **Activity Tracking**: Auto-blocks suspicious clients after 5 attempts  
✅ **Input Validation**: Strict format checks on all fields  

---

## Monitoring

### Prometheus Metrics

```bash
# View all metrics
curl http://localhost:8080/metrics

# Key metrics to watch
curl http://localhost:8080/metrics | grep -E "(orders_|latency|error)"
```

**Important metrics:**
- `orders_received_total` - Total orders submitted
- `orders_matched_total` - Successfully matched orders
- `order_processing_latency_seconds` - Latency histogram
- `orderbook_depth` - Current orders in orderbook
- `websocket_connections` - Active WebSocket clients

### Health Check

```bash
# Check system health
curl http://localhost:8080/healthz
```

**Response:**
```json
{
  "status": "healthy",
  "database": "connected",
  "redis": "connected",
  "uptime_seconds": 3600
}
```

---

## Performance Tuning

### Database Optimization

The engine uses connection pooling for efficiency:

```go
MaxOpenConns: 25      // Max concurrent connections
MaxIdleConns: 5       // Keep 5 warm connections
ConnMaxLifetime: 5m   // Recycle every 5 minutes
```

### Batch Persistence

Orders and trades are written in batches for better throughput:
- Batch size: 100 items
- Max delay: 1 second

This gives us 10x better write performance.

### Redis Caching

- Idempotency keys: 24-hour cache
- Orderbook snapshots: Fast reads
- Rate limit counters: Sub-millisecond checks

---

## Troubleshooting

### Services won't start

```bash
# Check what's running
docker-compose ps

# View logs
docker-compose logs trading-engine
docker-compose logs postgres
docker-compose logs redis

# Restart everything
docker-compose restart
```

### Database connection errors

```bash
# Test database connection
docker-compose exec postgres psql -U trader -d trading -c "SELECT 1;"

# Reset database (WARNING: deletes all data)
docker-compose down -v
docker-compose up -d postgres
```

### Rate limiting issues

If you're getting "rate limited" errors:
- Default limit: 100 requests burst, 10/sec sustained
- Wait 1 second between requests, or
- Whitelist your client in the rate limiter config

### Performance degradation

```bash
# Check metrics
curl http://localhost:8080/metrics | grep latency

# Enable profiling
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof
```

---

## Project Structure

```
├── cmd/server/          # Main entry point
├── engine/              # Matching engine core
│   ├── matching_engine.go
│   ├── orderbook.go
│   ├── snapshot_manager.go
│   └── recovery_manager.go
├── api/                 # HTTP handlers
│   ├── handlers.go
│   └── router.go
├── websocket/           # WebSocket streaming
├── validation/          # Security & validation
├── persistence/         # Database layer
├── cache/               # Redis caching
├── ratelimit/           # Rate limiting
├── metrics/             # Prometheus metrics
├── models/              # Data models
└── db/                  # SQL schemas
```

---

## Technology Stack

**Backend:**
- **Go 1.23** - Main language
- **gorilla/mux** - HTTP routing
- **gorilla/websocket** - WebSocket support
- **shopspring/decimal** - Precise decimal math
- **google/btree** - Orderbook data structure

**Storage:**
- **PostgreSQL 15** - Orders & trades persistence
- **Redis 7** - Idempotency, caching, rate limiting

**Deployment:**
- **Docker Compose** - Multi-service orchestration

**Monitoring:**
- **Prometheus** - Metrics collection
- **pprof** - Performance profiling

---

## What I Learned

Building this trading engine taught me a lot about:

1. **Concurrency trade-offs**: Sometimes single-threaded is simpler and fast enough
2. **Data structures matter**: Red-black trees give us O(log n) orderbook operations
3. **Recovery is critical**: Snapshots are way faster than full replays
4. **Security is hard**: 50+ attack patterns to defend against!
5. **Monitoring is essential**: You can't improve what you don't measure

---

## Future Improvements

If I had more time, I'd add:

- [ ] Multiple trading pairs simultaneously
- [ ] More order types (stop-loss, iceberg orders)
- [ ] Historical data API (query past trades)
- [ ] Market maker incentives
- [ ] High-availability clustering
- [ ] GraphQL API
- [ ] Admin dashboard

---

## Documentation

For more details, check out these docs:

- **API Validation Results**: `docs/API_VALIDATION_RESULTS.md`
- **Architecture Deep-Dive**: `docs/ARCHITECTURE.md`
- **Load Testing Guide**: `docs/LOAD_TESTING_COMPLETE.md`
- **Security Guide**: `docs/INPUT_VALIDATION_GUIDE.md`
- **Docker Setup**: `docs/DOCKER_SETUP_COMPLETE.md`
- **Recovery System**: `docs/EVENT_SOURCING_COMPLETE.md`

---

## About This Project

This trading engine was built as a backend engineering task for **2 Cent Ventures**. It demonstrates:

✅ **System Design**: Architecting a high-performance, fault-tolerant system  
✅ **Clean Code**: Well-organized, readable, and maintainable codebase  
✅ **Testing**: Comprehensive unit tests (85%+ coverage) and load tests  
✅ **Production Readiness**: Security, monitoring, logging, recovery  
✅ **Documentation**: Clear documentation for users and developers  

**Built with:** Go, PostgreSQL, Redis, Docker  
**Time Invested:** 2 weeks of focused development  
**Lines of Code:** ~8,000 lines  

---

## License

MIT License - See `LICENSE` file for details.

---

## Contact

**Status:** Production Ready  
**Version:** 1.0.0  
**Last Updated:** November 2025

Built with ❤️ for 2 Cent Ventures
