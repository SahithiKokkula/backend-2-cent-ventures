/**
 * K6 Load Test Script for Trading Engine - Order Submission
 * 
 * Tests POST /orders endpoint with configurable load patterns
 * Measures latency percentiles (p50, p95, p99) and error rates
 * 
 * Usage:
 *   # Basic test (50 VUs, 30 seconds)
 *   k6 run orders-load-test.js
 * 
 *   # Custom rate (2000 orders/sec)
 *   k6 run --vus 100 --duration 60s orders-load-test.js
 * 
 *   # Sustained load scenario
 *   k6 run --stage "0s:10" --stage "60s:10" orders-load-test.js
 * 
 *   # With environment variables
 *   k6 run -e API_URL=http://localhost:8080 -e TARGET_RATE=2000 orders-load-test.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { randomIntBetween, randomItem } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

// ============================================================================
// Configuration
// ============================================================================

const API_URL = __ENV.API_URL || 'http://localhost:8080';
const TARGET_RATE = parseInt(__ENV.TARGET_RATE || '2000'); // orders per second
const INSTRUMENT = __ENV.INSTRUMENT || 'BTC-USD';
const MIN_PRICE = parseFloat(__ENV.MIN_PRICE || '50000');
const MAX_PRICE = parseFloat(__ENV.MAX_PRICE || '51000');
const SPREAD = parseFloat(__ENV.SPREAD || '0.0005');
const MARKET_ORDER_RATIO = parseFloat(__ENV.MARKET_ORDER_RATIO || '0.05');

// ============================================================================
// Test Scenarios
// ============================================================================

export const options = {
  // Scenario 1: Sustained Load (default)
  scenarios: {
    sustained_load: {
      executor: 'constant-vus',
      vus: 50,
      duration: '30s',
      gracefulStop: '5s',
      tags: { test_type: 'sustained' },
    },
  },
  
  // Thresholds (SLA requirements)
  thresholds: {
    'http_req_duration': ['p(95)<500', 'p(99)<1000'], // 95% < 500ms, 99% < 1s
    'http_req_failed': ['rate<0.05'],                  // Error rate < 5%
    'order_submission_rate': ['rate>1000'],            // Min 1000 orders/sec
    'checks': ['rate>0.95'],                           // 95% of checks pass
  },
  
  // Summary configuration
  summaryTrendStats: ['min', 'avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

// Alternative scenarios (comment out 'sustained_load' above and use these)
export const SCENARIOS = {
  // Scenario 2: Ramp-up Load Test
  ramp_up: {
    executor: 'ramping-vus',
    startVUs: 0,
    stages: [
      { duration: '10s', target: 10 },   // Warm-up
      { duration: '30s', target: 50 },   // Ramp to 50 VUs
      { duration: '60s', target: 100 },  // Ramp to 100 VUs (high load)
      { duration: '30s', target: 100 },  // Sustain high load
      { duration: '20s', target: 0 },    // Ramp down
    ],
    gracefulStop: '5s',
    tags: { test_type: 'ramp_up' },
  },
  
  // Scenario 3: Spike Test (burst load)
  spike_test: {
    executor: 'ramping-vus',
    startVUs: 10,
    stages: [
      { duration: '10s', target: 10 },   // Normal load
      { duration: '5s', target: 200 },   // Spike to 200 VUs
      { duration: '20s', target: 200 },  // Sustain spike
      { duration: '5s', target: 10 },    // Return to normal
      { duration: '10s', target: 10 },   // Sustain normal
    ],
    gracefulStop: '5s',
    tags: { test_type: 'spike' },
  },
  
  // Scenario 4: Stress Test (find limits)
  stress_test: {
    executor: 'ramping-vus',
    startVUs: 10,
    stages: [
      { duration: '30s', target: 50 },   // Ramp to 50
      { duration: '30s', target: 100 },  // Ramp to 100
      { duration: '30s', target: 150 },  // Ramp to 150
      { duration: '30s', target: 200 },  // Ramp to 200
      { duration: '30s', target: 250 },  // Ramp to 250 (stress)
      { duration: '60s', target: 250 },  // Sustain stress
      { duration: '30s', target: 0 },    // Ramp down
    ],
    gracefulStop: '10s',
    tags: { test_type: 'stress' },
  },
  
  // Scenario 5: Constant Rate (precise rate control)
  constant_rate: {
    executor: 'constant-arrival-rate',
    rate: TARGET_RATE,                   // 2000 iterations/sec
    timeUnit: '1s',
    duration: '60s',
    preAllocatedVUs: 100,
    maxVUs: 500,
    gracefulStop: '5s',
    tags: { test_type: 'constant_rate' },
  },
};

// ============================================================================
// Custom Metrics
// ============================================================================

const orderSuccessRate = new Rate('order_success_rate');
const orderErrorRate = new Rate('order_error_rate');
const orderRateLimitedRate = new Rate('order_rate_limited_rate');
const orderSubmissionRate = new Rate('order_submission_rate');
const orderLatency = new Trend('order_latency_ms');
const limitOrdersCounter = new Counter('limit_orders_total');
const marketOrdersCounter = new Counter('market_orders_total');

// ============================================================================
// Order Generation Functions
// ============================================================================

/**
 * Generate a random price with normal distribution around mid-price
 */
function generatePrice(side) {
  const midPrice = (MIN_PRICE + MAX_PRICE) / 2;
  const priceRange = MAX_PRICE - MIN_PRICE;
  const stdDev = priceRange * 0.2;
  
  // Box-Muller transform for normal distribution
  const u1 = Math.random();
  const u2 = Math.random();
  const z = Math.sqrt(-2 * Math.log(u1)) * Math.cos(2 * Math.PI * u2);
  let price = midPrice + (z * stdDev);
  
  // Adjust for spread
  if (side === 'buy') {
    price -= midPrice * (SPREAD / 2);
  } else {
    price += midPrice * (SPREAD / 2);
  }
  
  // Clamp to range
  price = Math.max(MIN_PRICE, Math.min(MAX_PRICE, price));
  
  return Math.round(price * 100) / 100; // 2 decimal places
}

/**
 * Generate a random quantity with log-normal distribution
 */
function generateQuantity() {
  const QTY_MIN = 0.001;
  const QTY_MAX = 10.0;
  
  const mu = (QTY_MIN + QTY_MAX) / 4;
  const sigma = QTY_MAX / 3;
  
  // Log-normal distribution
  const u1 = Math.random();
  const u2 = Math.random();
  const z = Math.sqrt(-2 * Math.log(u1)) * Math.cos(2 * Math.PI * u2);
  let qty = Math.exp(Math.log(mu) + (sigma * z / mu));
  
  // Clamp to range
  qty = Math.max(QTY_MIN, Math.min(QTY_MAX, qty));
  
  return Math.round(qty * 100000000) / 100000000; // 8 decimal places
}

/**
 * Generate a random order (limit or market)
 */
function generateOrder() {
  const side = Math.random() < 0.5 ? 'buy' : 'sell';
  const isMarketOrder = Math.random() < MARKET_ORDER_RATIO;
  const orderType = isMarketOrder ? 'market' : 'limit';
  
  const order = {
    client_id: `k6_user_${__VU}_${__ITER}`,
    instrument: INSTRUMENT,
    side: side,
    type: orderType,
    quantity: generateQuantity(),
  };
  
  // Limit orders have price
  if (orderType === 'limit') {
    order.price = generatePrice(side);
    limitOrdersCounter.add(1);
  } else {
    marketOrdersCounter.add(1);
  }
  
  return order;
}

// ============================================================================
// Test Setup and Teardown
// ============================================================================

export function setup() {
  console.log('=== K6 Load Test Configuration ===');
  console.log(`API URL: ${API_URL}`);
  console.log(`Target Rate: ${TARGET_RATE} orders/sec`);
  console.log(`Instrument: ${INSTRUMENT}`);
  console.log(`Price Range: ${MIN_PRICE} - ${MAX_PRICE}`);
  console.log(`Market Order Ratio: ${MARKET_ORDER_RATIO * 100}%`);
  console.log('==================================\n');
  
  // Health check
  const healthRes = http.get(`${API_URL}/health`);
  if (healthRes.status !== 200) {
    console.error('Health check failed - server may be down');
    console.error(`Status: ${healthRes.status}`);
    console.error(`Body: ${healthRes.body}`);
  } else {
    console.log('Health check passed - server is ready');
  }
  
  return { startTime: new Date().toISOString() };
}

export function teardown(data) {
  console.log('\n=== Test Complete ===');
  console.log(`Started: ${data.startTime}`);
  console.log(`Ended: ${new Date().toISOString()}`);
  console.log('=====================\n');
}

// ============================================================================
// Main Test Function (runs for each VU iteration)
// ============================================================================

export default function() {
  // Generate order
  const order = generateOrder();
  
  // Prepare request
  const url = `${API_URL}/orders`;
  const payload = JSON.stringify(order);
  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
    tags: {
      order_type: order.type,
      side: order.side,
    },
  };
  
  // Submit order
  const startTime = new Date();
  const response = http.post(url, payload, params);
  const duration = new Date() - startTime;
  
  // Record metrics
  orderLatency.add(duration);
  orderSubmissionRate.add(1);
  
  // Check response
  const checkResult = check(response, {
    'status is 200 or 201': (r) => r.status === 200 || r.status === 201,
    'status is 202 (accepted)': (r) => r.status === 202,
    'response has order ID': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.order_id !== undefined;
      } catch (e) {
        return false;
      }
    },
    'response time < 500ms': (r) => r.timings.duration < 500,
    'response time < 1000ms': (r) => r.timings.duration < 1000,
  });
  
  // Track success/error rates
  if (response.status >= 200 && response.status < 300) {
    orderSuccessRate.add(1);
  } else if (response.status === 429) {
    orderRateLimitedRate.add(1);
    orderErrorRate.add(1);
  } else {
    orderErrorRate.add(1);
  }
  
  // Log errors (sample)
  if (response.status >= 400 && __ITER % 100 === 0) {
    console.log(` Error: ${response.status} - ${response.body}`);
  }
  
  // Sleep removed to test maximum throughput
  // Original sleep logic throttled requests to match target rate
  // Now running at full speed to measure actual capacity
}

// ============================================================================
// Custom Summary Handler
// ============================================================================

export function handleSummary(data) {
  const summary = {
    stdout: textSummary(data, { indent: '  ', enableColors: true }),
  };
  
  // Also save JSON report
  summary['load-test-results.json'] = JSON.stringify(data, null, 2);
  
  // Save HTML report (if needed)
  // summary['load-test-results.html'] = htmlReport(data);
  
  return summary;
}

function textSummary(data, options = {}) {
  const indent = options.indent || '';
  const enableColors = options.enableColors || false;
  
  let output = '\n';
  output += `${indent}=== LOAD TEST RESULTS ===\n\n`;
  
  // Test duration
  const metrics = data.metrics;
  if (metrics.iterations) {
    output += `${indent}Iterations: ${metrics.iterations.values.count}\n`;
    output += `${indent}Virtual Users: ${data.state ? data.state.testRunDurationMs : 'N/A'}\n\n`;
  }
  
  // HTTP metrics
  if (metrics.http_req_duration) {
    output += `${indent}HTTP Request Duration:\n`;
    output += `${indent}  Min:    ${metrics.http_req_duration.values.min.toFixed(2)} ms\n`;
    output += `${indent}  Avg:    ${metrics.http_req_duration.values.avg.toFixed(2)} ms\n`;
    output += `${indent}  Median: ${metrics.http_req_duration.values.med.toFixed(2)} ms\n`;
    output += `${indent}  P90:    ${metrics.http_req_duration.values['p(90)'].toFixed(2)} ms\n`;
    output += `${indent}  P95:    ${metrics.http_req_duration.values['p(95)'].toFixed(2)} ms\n`;
    output += `${indent}  P99:    ${metrics.http_req_duration.values['p(99)'].toFixed(2)} ms\n`;
    output += `${indent}  Max:    ${metrics.http_req_duration.values.max.toFixed(2)} ms\n\n`;
  }
  
  // Success/Error rates
  if (metrics.order_success_rate) {
    const successRate = metrics.order_success_rate.values.rate * 100;
    output += `${indent}Order Success Rate: ${successRate.toFixed(2)}%\n`;
  }
  if (metrics.order_error_rate) {
    const errorRate = metrics.order_error_rate.values.rate * 100;
    output += `${indent}Order Error Rate:   ${errorRate.toFixed(2)}%\n`;
  }
  if (metrics.order_rate_limited_rate) {
    const rateLimitedRate = metrics.order_rate_limited_rate.values.rate * 100;
    output += `${indent}Rate Limited:       ${rateLimitedRate.toFixed(2)}%\n\n`;
  }
  
  // Order counts
  if (metrics.limit_orders_total) {
    output += `${indent}Limit Orders:  ${metrics.limit_orders_total.values.count}\n`;
  }
  if (metrics.market_orders_total) {
    output += `${indent}Market Orders: ${metrics.market_orders_total.values.count}\n\n`;
  }
  
  // Throughput
  if (metrics.http_reqs) {
    const totalReqs = metrics.http_reqs.values.count;
    const duration = metrics.http_req_duration ? metrics.http_req_duration.values.count : 1;
    const throughput = totalReqs / (duration / 1000);
    output += `${indent}Throughput: ${throughput.toFixed(2)} orders/sec\n`;
  }
  
  output += `${indent}\n=========================\n`;
  
  return output;
}
