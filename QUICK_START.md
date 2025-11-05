# Quick Start Guide

> Get the trading engine running in under 2 minutes!

## The Absolute Fastest Way (Docker)

**Step 1:** Make sure Docker is running on your machine

**Step 2:** Open a terminal and run:

```bash
docker-compose up -d
```

**Step 3:** Wait 10 seconds for services to start

**Step 4:** Test it!

```bash
# Place a buy order
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
```

🎉 **Done!** Your trading engine is running.

---

## Essential Endpoints

| What You Want | How to Do It |
|---------------|--------------|
| **Place an order** | `POST /orders` with order details |
| **Check orderbook** | `GET /orderbook?instrument=BTCUSD` |
| **Get recent trades** | `GET /trades?limit=20` |
| **Cancel an order** | `POST /orders/{order_id}/cancel` |
| **Check if it's working** | `GET /healthz` |
| **See performance metrics** | `GET /metrics` |

---

## Quick Test Scenario

Here's how to test the matching engine in 30 seconds:

```bash
# 1. Place a buy order (Alice wants to buy at $70,000)
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

# 2. Place a sell order (Bob wants to sell at $70,000)
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "client_id": "bob",
    "instrument": "BTCUSD",
    "side": "sell",
    "type": "limit",
    "price": 70000.00,
    "quantity": 0.3
  }'

# 3. Check trades (should show a match!)
curl "http://localhost:8080/trades?instrument=BTCUSD"
```

**What happened?** Bob's sell order matched Alice's buy order at $70,000! Alice got 0.3 BTC (Bob's full order), and she still has 0.2 BTC remaining in her order.

---

## Docker Commands Cheat Sheet

```bash
# Start services
docker-compose up -d

# Stop services
docker-compose down

# View logs
docker-compose logs -f trading-engine

# Restart everything
docker-compose restart

# Delete everything and start fresh
docker-compose down -v
docker-compose up -d
```

---

## Order Fields Explained

When placing an order, you need these fields:

| Field | What It Is | Example | Rules |
|-------|------------|---------|-------|
| `client_id` | Who's placing the order | `"alice"` | 1-64 characters, letters/numbers/`_`/`-` only |
| `instrument` | What you're trading | `"BTCUSD"` | Uppercase, max 20 chars |
| `side` | Buy or sell | `"buy"` or `"sell"` | Must be exactly "buy" or "sell" |
| `type` | Order type | `"limit"` or `"market"` | Limit = specific price, Market = best available |
| `price` | Your price | `70000.50` | Required for limit orders, max 8 decimals |
| `quantity` | How much | `0.25` | Must be positive, max 8 decimals |

**Validation limits:**
- Price: Between 0.00000001 and 1,000,000,000
- Quantity: Between 0.00000001 and 1,000,000,000

---

## WebSocket Quick Start

Want real-time updates? Connect to the WebSocket:

```javascript
const ws = new WebSocket('ws://localhost:8081/stream');

ws.onopen = () => {
    // Subscribe to trade updates
    ws.send(JSON.stringify({
        action: 'subscribe',
        channels: ['trades', 'orderbook_deltas']
    }));
};

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log('Live update:', data);
};
```

**Available channels:**
- `trades` - See trades as they happen
- `orderbook_deltas` - See orderbook changes
- `orders` - Get order confirmations

---

## Running Without Docker

If you want to run locally without Docker:

**Prerequisites:**
- Go 1.23+
- PostgreSQL 15+
- Redis 7+

**Steps:**

```bash
# 1. Install dependencies
go mod download

# 2. Set up environment variables
cp .env.example .env
# Edit .env with your database credentials

# 3. Run database migrations
psql -U trader -d trading -f db/schema.sql

# 4. Build and run
go build -o bin/trading-engine ./cmd/server
./bin/trading-engine
```

---

## Performance Testing

Want to see how fast it is?

```bash
# Run the Python load tester
python load-test/simple-load-test.py

# Expected output:
# Throughput: ~1,700 orders/sec
# Latency p95: ~45ms
# Success rate: 99%+
```

---

## Troubleshooting

**Problem:** "Connection refused"
```bash
# Solution: Check if services are running
docker-compose ps

# Restart if needed
docker-compose restart trading-engine
```

**Problem:** "Rate limited"
- Default limit: 100 requests burst, then 10/sec sustained
- Solution: Wait 1 second between requests

**Problem:** Database errors
```bash
# Solution: Reset database
docker-compose down -v
docker-compose up -d
```

---

## What's Running?

When you start with Docker, you get:

| Service | Port | What It Does |
|---------|------|--------------|
| **Trading Engine** | 8080 | Main HTTP API |
| **WebSocket** | 8081 | Real-time streaming |
| **PostgreSQL** | 5432 | Database (orders & trades) |
| **Redis** | 6379 | Caching & idempotency |
| **Metrics** | 8080/metrics | Prometheus metrics |

---

## Next Steps

Once you have it running:

1. ✅ Try the test scenario above (Alice and Bob)
2. ✅ Check the orderbook: `curl http://localhost:8080/orderbook?instrument=BTCUSD`
3. ✅ View metrics: `curl http://localhost:8080/metrics`
4. ✅ Connect via WebSocket for live updates
5. ✅ Run the load test to see performance

For complete API documentation, see the main **README.md** file.

---

**Need help?** Check the main README for detailed docs, or look in the `docs/` folder for guides on specific topics.

Happy trading! 🚀
