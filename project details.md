[![Review Assignment Due Date](https://classroom.github.com/assets/deadline-readme-button-22041afd0340ce965d47ae6ef1cefeee28c7c493a6346c4f15d667ab976d596c.svg)](https://classroom.github.com/a/1tQYVu8h)
# Backend Engineering Assignment — Real-Time Trade Clearing & Analytics Engine
Please submit the Google Form once the assignment is completed.

 [Submit Here](https://docs.google.com/forms/d/e/1FAIpQLSfUngCIE4Bqdtq6wTMJTRQ4g2bbzSw7dt3yFiTWIpFd1Q8sog/viewform)


Estimated candidate effort: 6–12 hours (hard, production-level task).
**Primary skills tested:** system design, API design, concurrency, databases, streaming, performance, reliability, security, testing.

---

### 1. Objective (one sentence)

Design and implement a scalable backend service that ingests streamed trade orders (via HTTP + WebSocket), performs matching/clearing for a simplified exchange, persists trade history and order book snapshots, exposes low-latency APIs and metrics for real-time analytics, and demonstrates robustness under load and failure.

---

### 2. Suggested stack (candidates may choose alternatives but must justify)

* **Language:** Go, Java, or Node.js.
* **Database(s):** Relational (Postgres) plus a fast KV store (Redis). Candidates may use only Postgres if they justify tradeoffs.
* **Messaging/Streaming:** Kafka / NATS / Redis Streams (or in-memory simulated queue for single-node impl).
* **Containerization:** Docker.
* **Observability:** Prometheus-compatible metrics + basic logging.
* **Testing:** Unit tests + basic load tests (k6, artillery, or a simple script).

---

### 3. High-level features (must implement)

* **Order ingestion**
    * HTTP `POST` endpoint for placing orders.
    * WebSocket feed for receiving market orders (optional for candidate — but at least one streaming ingestion mechanism required) using the Binance’s Websockets.
* **Order types supported**
    * Limit orders, Market orders, Cancel order.
    * For Limit orders: price, quantity, side (buy/sell), client\_id.
* **Matching engine**
    * Single-instrument (e.g., `BTC-USD`) order matching engine that:
        * Matches market orders immediately against best available limit orders.
        * Matches limit orders according to price-time priority.
        * Partial fills allowed.
        * Produces executions (trades) with unique trade IDs.
* **Persistence**
    * Persist orders, order state changes, executions/trades, and periodic order-book snapshots.
    * Ensure durability and correctness across restarts.
* **Concurrency & correctness**
    * Must handle concurrent order submissions without double allocation or lost updates.
    * Demonstrate how the engine avoids race conditions (locks, optimistic concurrency, single-threaded matching loop, etc.).
* **Public read APIs**
    * `GET /orderbook` — returns top N bids & asks with totals and cumulative depth.
    * `GET /trades?limit=50` — most recent N trades.
    * `GET /orders/{order_id}` — order state.
* **Client events**
    * WebSocket or Server-Sent Events (SSE) broadcasting:
        * Orderbook deltas, new trades, and order state changes.
* **Admin/operational endpoints**
    * Health check: `/healthz`.
    * Metrics endpoint: `/metrics` (Prometheus).
    * Ability to request an on-demand order-book snapshot persisted to DB.
* **Idempotency & resilience**
    * API must support idempotent order submission (idempotency key).
    * Proper handling and logging of transient DB/network errors with retry/backoff where appropriate.

---

### 4. Detailed functional requirements & constraints

**4.1 Order model**
Each order must include:
* `order_id` (client-provided or server-generated UUID)
* `client_id`
* `instrument` (e.g., `BTC-USD`)
* `side` (buy | sell)
* `type` (limit | market)
* `price` (for limit orders)
* `quantity`
* `filled_quantity`
* `status` (open, partially_filled, filled, cancelled, rejected)
* `created_at`, `updated_at`

**4.2 Matching rules**
* **Price-time priority**
    * Bids sorted by price desc, earliest timestamp first.
    * Asks sorted by price asc, earliest timestamp first.
* **Market orders**
    * Match until quantity filled or book exhausted; produce partial fills and `remaining_quantity`.
* **Zero-quantity level removal**
    * When a price level reaches zero, remove it from the in-memory book and persist change.
* **Trade record**
    * For each match produce a trade with `trade_id`, `buy_order_id`, `sell_order_id`, `price`, `quantity`, `timestamp`.

**4.3 Persistence & recovery**
* On restart, the system must be able to:
    * Rebuild or reload the last persisted order-book snapshot + unapplied events to reach current state, OR
    * Reconstruct state by replaying persisted open orders and unfilled quantities.
* Candidate must document recovery approach and trade-offs.

**4.4 Performance targets (soft but measurable)**
* Single-node implementation must handle 2,000 orders/sec sustained with sub-100ms median latency for order acceptance under that load (approximate targets — candidates must measure and report).
* Provide a simple load-test harness and results demonstrating the system under stress.

**4.5 Consistency**
* Ensure no double fills and accurate `filled_quantity` even under concurrent requests.
* If using a DB transaction, demonstrate proper isolation level and reasoning.

**4.6 Security & validation**
* Validate order parameters (positive quantities, price precision).
* Soften risks: no auth required for this assignment but input validation and basic rate limiting are required.

---

### 5. Non-functional requirements

**5.1 Code quality**
* Clear module boundaries; separation between API layer, matching engine, persistence adapter, and broadcast/stream layer.
* Tests: unit tests for matching logic + integration tests for API flows.

**5.2 Observability & diagnostics**
* `/metrics` exposes counters/histograms:
    * `orders_received_total`, `orders_matched_total`, `orders_rejected_total`
    * `order_latency_seconds` histogram
    * `current_orderbook_depth`
* Log important events: order submitted, order matched (trade details), order cancelled, errors.

**5.3 Reliability**
* Demonstrate how system handles:
    * Database disconnect and reconnection
    * Crashed matching worker + restart (state recovery)
    * Duplicate order submissions (idempotency)

**5.4 Deployment**
* Provide a `Dockerfile` and a `docker-compose.yml` to run the service with Postgres (and Redis/Kafka if used).

---

### 6. Data & test fixtures (must include)

* Provide a script `fixtures/gen_orders.py|js` that generates:
    * 100k realistic limit orders across a price band
    * A burst of market orders to test matching
* Provide a `load-test/` folder with a script (k6 or simple Node script) to submit orders concurrently and record latency.

---

### 7. Deliverables (exact)

* Git repo (private) with source code, tests, and instructions.
* `README.md` containing:
    * How to build and run locally (docker compose).
    * How to run tests and load tests.
    * Design doc (1–2 pages) describing architecture, concurrency model, recovery strategy, and tradeoffs.
    * Postman collection or curl examples for all major endpoints.
    * Short report (1 page) with load-test results and how the candidate would scale to multi-node / multi-instrument in production.
* Optional: small demo video (2–5 minutes) showing the system under load and key APIs.

---

### 8. Evaluation rubric (recommended weights)

* **Correctness (25%)** — Matching rules, persistence correctness, no double fills, idempotency.
* **Concurrency & Robustness (20%)** — Correct handling of concurrent submissions, recovery from failures.
* **Performance (15%)** — Load-test evidence and latency numbers.
* **Code Quality & Tests (15%)** — Clean code, modularity, unit/integration tests.
* **API Design & Documentation (10%)** — Usability of APIs, completeness of README and examples.
* **Observability & Operational Readiness (10%)** — Metrics, health checks, logs.
* **Bonus (5%)** — Extra features (multi-instrument, snapshots, client auth, persistence using WAL/Event-sourcing).

---

### 9. Bonus / Extra-credit tasks (optional)

* Multi-instrument support with partitioned matching workers.
* Event-sourcing: persist only events (orders/commands) and derive state by replay.
* Implement a simple settlement service that net positions per `client_id` at end of day.
* Implement role-based API keys and rate-limiting per client.
* Add an analytics endpoint that returns VWAP, 1-min/5-min trade aggregates.

---

### 10. Example API specification (minimal — include in README)

* **`POST /orders`**
    * Body:
    ```json
    {
      "idempotency_key": "abc-123",
      "order_id": "order-1",       // optional
      "client_id": "client-A",
      "instrument": "BTC-USD",
      "side": "buy",
      "type": "limit",
      "price": 70150.5,
      "quantity": 0.25
    }
    ```

* **`POST /orders/{order_id}/cancel`**
    * Cancels the order and returns final state.

* **`GET /orderbook?instrument=BTC-USD&levels=20`**
    * Returns top N bids and asks with cumulative totals.

* **`GET /trades?limit=50`**

* **`WS /stream`**
    * Client subscribes to `orderbook_deltas`, `trades`, `orders`.

---

### 11. Suggested hidden test cases / scoring tests (for your internal use)

* Submit concurrent limit orders at the same price from 1,000 clients and verify total quantity and no overlaps.
* Submit market orders that exhaust parts of the book and verify correct partial fills and remaining orders.
* Simulate DB outage: bring down DB for 10s and verify how service behaves & recovers.
* Duplicate `POST` with same idempotency key — service returns same result and does not match twice.
* Load test to target 2k orders/sec sustained for 1 minute and record latency percentiles.
