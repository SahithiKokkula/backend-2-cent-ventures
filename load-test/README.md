# Load Test Scenarios for Trading Engine

This directory contains k6 load testing scripts for the Trading Engine API.

## Quick Start

### 1. Install k6

**Windows (Chocolatey):**
```powershell
choco install k6
```

**Windows (Manual):**
Download from https://k6.io/docs/get-started/installation/

**Linux/Mac:**
```bash
# macOS
brew install k6

# Ubuntu/Debian
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6
```

### 2. Start Trading Engine Server

```powershell
# From trading-engine directory
go run cmd/server/main.go
```

Or use the compiled binary:
```powershell
.\server.exe
```

### 3. Run Load Tests

**Basic Test (50 VUs, 30 seconds):**
```powershell
k6 run load-test/orders-load-test.js
```

**High Load Test (100 VUs, 60 seconds):**
```powershell
k6 run --vus 100 --duration 60s load-test/orders-load-test.js
```

**Custom Configuration:**
```powershell
k6 run -e API_URL=http://localhost:8080 -e TARGET_RATE=2000 load-test/orders-load-test.js
```

---

## Test Scenarios

### 1. Sustained Load (Default)
**Goal:** Verify stable performance under constant load  
**Duration:** 30 seconds  
**VUs:** 50  
**Expected Rate:** ~1500-2000 orders/sec

```powershell
k6 run load-test/orders-load-test.js
```

**Success Criteria:**
- P95 latency < 500ms
- P99 latency < 1000ms
- Error rate < 5%
- Throughput > 1000 orders/sec

---

### 2. Ramp-Up Load Test
**Goal:** Find performance degradation point  
**Duration:** 150 seconds  
**VUs:** 0 → 10 → 50 → 100 → 0

To use this scenario, edit `orders-load-test.js`:
```javascript
// Comment out the 'sustained_load' scenario
// Uncomment the 'ramp_up' scenario from SCENARIOS object
export const options = {
  scenarios: {
    ramp_up: SCENARIOS.ramp_up,
  },
  // ... rest of options
};
```

Then run:
```powershell
k6 run load-test/orders-load-test.js
```

**What to Watch:**
- Latency increase as VUs ramp up
- Point where error rate increases
- Rate limiting kicks in (HTTP 429)

---

### 3. Spike Test (Burst Load)
**Goal:** Test recovery from sudden traffic spike  
**Duration:** 50 seconds  
**VUs:** 10 → 200 (spike) → 10

```javascript
// Use spike_test scenario in orders-load-test.js
export const options = {
  scenarios: {
    spike_test: SCENARIOS.spike_test,
  },
};
```

**Success Criteria:**
- System handles spike without crashes
- Recovers to normal latency after spike
- No data corruption during spike

---

### 4. Stress Test (Find Limits)
**Goal:** Find breaking point  
**Duration:** 240 seconds (4 minutes)  
**VUs:** 10 → 50 → 100 → 150 → 200 → 250 → 0

```javascript
// Use stress_test scenario in orders-load-test.js
export const options = {
  scenarios: {
    stress_test: SCENARIOS.stress_test,
  },
};
```

**What to Monitor:**
- CPU/Memory usage on server
- Point where latency exceeds thresholds
- Error rate increase
- Rate limiting behavior

---

### 5. Constant Rate (2000 orders/sec target)
**Goal:** Achieve precise 2000 orders/sec target  
**Duration:** 60 seconds  
**Rate:** 2000 iterations/sec  
**VUs:** Auto-scaled (100-500)

```javascript
// Use constant_rate scenario in orders-load-test.js
export const options = {
  scenarios: {
    constant_rate: SCENARIOS.constant_rate,
  },
};
```

Or with environment variable:
```powershell
k6 run -e TARGET_RATE=2000 load-test/orders-load-test.js
```

**Success Criteria:**
- Actual rate ≥ 1900 orders/sec (95% of target)
- P95 latency < 500ms at 2000 orders/sec
- Error rate < 5%

---

## Configuration Options

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `API_URL` | http://localhost:8080 | Trading Engine API base URL |
| `TARGET_RATE` | 2000 | Target orders per second |
| `INSTRUMENT` | BTC-USD | Trading instrument |
| `MIN_PRICE` | 50000 | Minimum order price |
| `MAX_PRICE` | 51000 | Maximum order price |
| `SPREAD` | 0.0005 | Bid-ask spread (0.05%) |
| `MARKET_ORDER_RATIO` | 0.05 | Ratio of market orders (5%) |

**Example:**
```powershell
k6 run `
  -e API_URL=http://localhost:8080 `
  -e TARGET_RATE=3000 `
  -e INSTRUMENT=ETH-USD `
  -e MIN_PRICE=3000 `
  -e MAX_PRICE=3100 `
  load-test/orders-load-test.js
```

### Command-Line Options

```powershell
# Custom VUs and duration
k6 run --vus 100 --duration 60s load-test/orders-load-test.js

# Stages (ramp-up pattern)
k6 run --stage "10s:10" --stage "30s:50" --stage "20s:0" load-test/orders-load-test.js

# Output results to file
k6 run --out json=results.json load-test/orders-load-test.js

# Quiet mode (less output)
k6 run --quiet load-test/orders-load-test.js

# Summary only
k6 run --no-usage-report --summary-export=summary.json load-test/orders-load-test.js
```

---

## Interpreting Results

### Latency Percentiles

```
HTTP Request Duration:
  Min:    12.45 ms       ← Fastest request
  Avg:    85.32 ms       ← Average latency
  Median: 78.21 ms       ← 50% of requests faster than this
  P90:    150.43 ms      ← 90% of requests faster than this
  P95:    185.67 ms      Target: < 500ms
  P99:    342.89 ms      Target: < 1000ms
  Max:    1205.34 ms     ← Slowest request
```

**What's Good:**
- P95 < 500ms → Most users get fast response
- P99 < 1000ms → Even slow requests acceptable
- Small gap between P95-P99 → Consistent performance

**What's Bad:**
- P95 > 500ms → Performance degradation
- P99 > 1000ms → Unacceptable slow requests
- Large P95-P99 gap → Inconsistent performance

### Success/Error Rates

```
Order Success Rate: 97.82%    Target: > 95%
Order Error Rate:   2.18%     Target: < 5%
Rate Limited:       1.45%     ℹ️  Expected under high load
```

**What's Good:**
- Success rate > 95% → System is stable
- Error rate < 5% → Acceptable failure rate
- ℹ️  Rate limiting < 10% → Server protecting itself

**What's Bad:**
- Success rate < 90% → System struggling
- Error rate > 10% → Something is wrong
-  Rate limiting > 20% → Too aggressive or load too high

### Throughput

```
Throughput: 1847.23 orders/sec
```

**Targets:**
- **Minimum:** 1000 orders/sec
- **Target:** 2000 orders/sec
- **Stretch:** 3000 orders/sec

**If Below Target:**
1. Check server CPU/memory
2. Increase concurrency (--vus)
3. Reduce sleep time in script
4. Check rate limiting configuration
5. Optimize server code (matching engine, DB)

---

## Monitoring During Tests

### 1. Prometheus Metrics
Open http://localhost:9090 and run queries:

```promql
# Orders per second
rate(orders_total[1m])

# Latency percentiles
histogram_quantile(0.95, rate(order_latency_seconds_bucket[1m]))
histogram_quantile(0.99, rate(order_latency_seconds_bucket[1m]))

# Error rate
rate(orders_failed_total[1m]) / rate(orders_total[1m])

# Rate limiting
rate(orders_rate_limited_total[1m])
```

### 2. Server Logs
Watch logs in real-time:
```powershell
Get-Content logs.txt -Wait
```

Look for:
- Error messages
- Rate limiting logs
- Slow queries
- Connection errors

### 3. System Resources
```powershell
# CPU/Memory usage
Get-Process server | Select-Object CPU, WorkingSet64

# Or use Task Manager
```

---

## Troubleshooting

### High Error Rate (> 5%)

**Possible Causes:**
1. Server overloaded → Reduce VUs or rate
2. Rate limiting too aggressive → Check server config
3. Database slow → Check DB performance
4. Connection issues → Check network

**Solutions:**
```powershell
# Reduce load
k6 run --vus 25 --duration 30s load-test/orders-load-test.js

# Check rate limits
curl http://localhost:8080/health

# Increase server limits (config.yaml)
# rate_limit:
#   orders_per_second: 5000
```

### High Latency (P95 > 500ms)

**Possible Causes:**
1. CPU bound → Optimize matching engine
2. Database slow → Add indexes, optimize queries
3. GC pauses → Tune Go GC settings
4. Lock contention → Review concurrent code

**Solutions:**
- Profile server: `go tool pprof`
- Check Prometheus metrics
- Review slow logs
- Optimize hot paths

### Rate Limiting (> 20%)

**Expected Behavior:**
- Rate limiting protects server from overload
- Should see HTTP 429 responses

**If Too Aggressive:**
Edit `config/config.yaml`:
```yaml
rate_limit:
  enabled: true
  orders_per_second: 5000    # Increase from 1000
  burst: 2000                # Allow bursts
```

**If Not Working:**
- Check rate limiter logs
- Verify configuration loaded
- Test with lower load first

---

## Best Practices

### 1. Start Small
```powershell
# Start with low load
k6 run --vus 10 --duration 30s load-test/orders-load-test.js

# Gradually increase
k6 run --vus 25 --duration 30s load-test/orders-load-test.js
k6 run --vus 50 --duration 30s load-test/orders-load-test.js
k6 run --vus 100 --duration 60s load-test/orders-load-test.js
```

### 2. Monitor Server
- Open Prometheus dashboard
- Watch server logs
- Monitor CPU/memory
- Check for errors

### 3. Run Multiple Tests
```powershell
# Sustained load
k6 run load-test/orders-load-test.js

# Spike test
# (edit script to use spike_test scenario)
k6 run load-test/orders-load-test.js

# Stress test
# (edit script to use stress_test scenario)
k6 run load-test/orders-load-test.js
```

### 4. Save Results
```powershell
# JSON output
k6 run --out json=results.json load-test/orders-load-test.js

# CSV output (with extension)
k6 run --out csv=results.csv load-test/orders-load-test.js

# Summary to file
k6 run --summary-export=summary.json load-test/orders-load-test.js
```

### 5. Compare Results
- Baseline test (before changes)
- Run test after each optimization
- Compare latencies, throughput, error rates
- Document improvements

---

## Example Test Run

```powershell
PS> k6 run load-test/orders-load-test.js

=== K6 Load Test Configuration ===
API URL: http://localhost:8080
Target Rate: 2000 orders/sec
Instrument: BTC-USD
Price Range: 50000 - 51000
Market Order Ratio: 5%
==================================

Health check passed - server is ready

running (0m30.5s), 00/50 VUs, 45387 complete and 0 interrupted iterations
sustained_load ✓ [======================================] 50 VUs  30s

=== LOAD TEST RESULTS ===

Iterations: 45387
Virtual Users: 50

HTTP Request Duration:
  Min:    10.23 ms
  Avg:    32.45 ms
  Median: 28.67 ms
  P90:    52.34 ms
  P95:    67.89 ms   Target: < 500ms
  P99:    145.23 ms  Target: < 1000ms
  Max:    456.78 ms

Order Success Rate: 98.45%   Target: > 95%
Order Error Rate:   1.55%    Target: < 5%
Rate Limited:       0.89%    Acceptable

Limit Orders:  43117
Market Orders: 2270

Throughput: 1488.33 orders/sec   Below 2000 target

=== Test Complete ===
```

---

## Next Steps

1. **Baseline Test:** Run sustained load test and record results
2. **Optimize:** Based on bottlenecks found
3. **Re-test:** Run again and compare results
4. **Stress Test:** Find system limits
5. **Production Ready:** When targets consistently met

---

## Files

- `orders-load-test.js` - Main k6 load test script
- `load-test-results.json` - Test results (auto-generated)
- `README.md` - This file

---

## References

- k6 Documentation: https://k6.io/docs/
- k6 Best Practices: https://k6.io/docs/testing-guides/
- Prometheus: http://localhost:9090
- Trading Engine API: http://localhost:8080
