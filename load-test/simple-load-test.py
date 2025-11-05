#!/usr/bin/env python3
"""
Simple Load Tester for Trading Engine (No k6 Required)

Uses Python's asyncio for concurrent requests
Measures latency percentiles and error rates

Usage:
    # Basic test (50 concurrent workers, 30 seconds, 2000 orders/sec target)
    python simple-load-test.py

    # Custom parameters
    python simple-load-test.py --api-url http://localhost:8080 --workers 100 --duration 60 --target-rate 3000

    # Burst test
    python simple-load-test.py --burst-mode --burst-workers 200 --burst-duration 10
"""

import argparse
import asyncio
import aiohttp
import json
import time
import random
import math
import sys
from dataclasses import dataclass, field
from typing import List, Dict
from datetime import datetime
import statistics


@dataclass
class Order:
    """Represents a trading order"""
    client_id: str
    instrument: str
    side: str
    type: str
    price: float = None
    quantity: float = None
    
    def to_dict(self) -> Dict:
        """Convert to dictionary for JSON serialization"""
        result = {
            "client_id": self.client_id,
            "instrument": self.instrument,
            "side": self.side,
            "type": self.type,
            "quantity": self.quantity,
        }
        # Limit orders have price, market orders don't
        if self.type == "limit" and self.price is not None:
            result["price"] = self.price
        return result


@dataclass
class RequestResult:
    """Result of a single HTTP request"""
    status_code: int
    latency_ms: float
    success: bool
    error: str = None


@dataclass
class LoadTestResults:
    """Aggregated results from load test"""
    total_requests: int = 0
    successful_requests: int = 0
    failed_requests: int = 0
    rate_limited_requests: int = 0
    latencies: List[float] = field(default_factory=list)
    status_codes: Dict[int, int] = field(default_factory=dict)
    start_time: float = None
    end_time: float = None
    
    def add_result(self, result: RequestResult):
        """Add a single request result"""
        self.total_requests += 1
        self.latencies.append(result.latency_ms)
        
        self.status_codes[result.status_code] = self.status_codes.get(result.status_code, 0) + 1
        
        if result.success:
            self.successful_requests += 1
        else:
            self.failed_requests += 1
            if result.status_code == 429:
                self.rate_limited_requests += 1
    
    def calculate_percentile(self, percentile: float) -> float:
        """Calculate latency percentile"""
        if not self.latencies:
            return 0.0
        sorted_latencies = sorted(self.latencies)
        index = int(len(sorted_latencies) * percentile / 100)
        return sorted_latencies[min(index, len(sorted_latencies) - 1)]
    
    def get_summary(self) -> Dict:
        """Get test summary"""
        duration = self.end_time - self.start_time if self.end_time and self.start_time else 0
        
        return {
            "duration_seconds": duration,
            "total_requests": self.total_requests,
            "successful_requests": self.successful_requests,
            "failed_requests": self.failed_requests,
            "rate_limited_requests": self.rate_limited_requests,
            "success_rate": self.successful_requests / self.total_requests * 100 if self.total_requests > 0 else 0,
            "error_rate": self.failed_requests / self.total_requests * 100 if self.total_requests > 0 else 0,
            "throughput_per_sec": self.total_requests / duration if duration > 0 else 0,
            "latency_min_ms": min(self.latencies) if self.latencies else 0,
            "latency_max_ms": max(self.latencies) if self.latencies else 0,
            "latency_avg_ms": statistics.mean(self.latencies) if self.latencies else 0,
            "latency_median_ms": statistics.median(self.latencies) if self.latencies else 0,
            "latency_p50_ms": self.calculate_percentile(50),
            "latency_p90_ms": self.calculate_percentile(90),
            "latency_p95_ms": self.calculate_percentile(95),
            "latency_p99_ms": self.calculate_percentile(99),
            "status_codes": self.status_codes,
        }
    
    def print_summary(self):
        """Print formatted test summary"""
        summary = self.get_summary()
        
        print("\n" + "="*60)
        print("LOAD TEST RESULTS")
        print("="*60)
        
        print(f"\n>Test Duration: {summary['duration_seconds']:.2f} seconds")
        print(f"   Total Requests: {summary['total_requests']:,}")
        print(f"   Throughput: {summary['throughput_per_sec']:.2f} orders/sec")
        
        print(f"\nSuccess Rate: {summary['success_rate']:.2f}%")
        print(f"   Successful: {summary['successful_requests']:,} ({summary['success_rate']:.1f}%)")
        print(f"   Failed: {summary['failed_requests']:,} ({summary['error_rate']:.1f}%)")
        if summary['rate_limited_requests'] > 0:
            rate_limited_pct = summary['rate_limited_requests'] / summary['total_requests'] * 100
            print(f"   Rate Limited: {summary['rate_limited_requests']:,} ({rate_limited_pct:.1f}%)")
        
        print(f"\n⏱️  Latency (milliseconds):")
        print(f"   Min:    {summary['latency_min_ms']:.2f} ms")
        print(f"   Avg:    {summary['latency_avg_ms']:.2f} ms")
        print(f"   Median: {summary['latency_median_ms']:.2f} ms")
        print(f"   P90:    {summary['latency_p90_ms']:.2f} ms")
        print(f"   P95:    {summary['latency_p95_ms']:.2f} ms", end="")
        if summary['latency_p95_ms'] < 500:
            print(" (< 500ms target)")
        else:
            print(" (> 500ms target)")
        print(f"   P99:    {summary['latency_p99_ms']:.2f} ms", end="")
        if summary['latency_p99_ms'] < 1000:
            print(" (< 1000ms target)")
        else:
            print(" (> 1000ms target)")
        print(f"   Max:    {summary['latency_max_ms']:.2f} ms")
        
        print(f"\n📈 Status Codes:")
        for status_code, count in sorted(summary['status_codes'].items()):
            pct = count / summary['total_requests'] * 100
            status_emoji = "✅" if 200 <= status_code < 300 else ("⚠️" if status_code == 429 else "❌")
            print(f"   {status_emoji} {status_code}: {count:,} ({pct:.1f}%)")
        
        print("\n" + "="*60)
        
        # SLA check
        print("\nSLA Targets:")
        targets_met = []
        targets_met.append(("P95 < 500ms", summary['latency_p95_ms'] < 500))
        targets_met.append(("P99 < 1000ms", summary['latency_p99_ms'] < 1000))
        targets_met.append(("Error rate < 5%", summary['error_rate'] < 5))
        targets_met.append(("Throughput > 1000/s", summary['throughput_per_sec'] > 1000))
        
        for target_name, met in targets_met:
            status = "PASS" if met else "FAIL"
            print(f"   {status}: {target_name}")
        
        all_met = all(met for _, met in targets_met)
        print(f"\n{'ALL TARGETS MET' if all_met else 'SOME TARGETS NOT MET'}")
        print("="*60 + "\n")


class OrderGenerator:
    """Generates realistic trading orders"""
    
    def __init__(self, instrument: str, min_price: float, max_price: float,
                 spread: float, market_order_ratio: float):
        self.instrument = instrument
        self.min_price = min_price
        self.max_price = max_price
        self.mid_price = (min_price + max_price) / 2
        self.spread = spread
        self.market_order_ratio = market_order_ratio
        self.order_count = 0
    
    def generate_price(self, side: str) -> float:
        """Generate price with normal distribution"""
        price_range = self.max_price - self.min_price
        std_dev = price_range * 0.2
        
        # Normal distribution
        price = random.gauss(self.mid_price, std_dev)
        
        # Adjust for spread
        if side == "buy":
            price -= self.mid_price * (self.spread / 2)
        else:
            price += self.mid_price * (self.spread / 2)
        
        # Clamp to range
        return max(self.min_price, min(self.max_price, round(price, 2)))
    
    def generate_quantity(self) -> float:
        """Generate quantity with log-normal distribution"""
        mu = 1.0
        sigma = 2.0
        qty = random.lognormvariate(math.log(mu), sigma)
        return max(0.001, min(10.0, round(qty, 8)))
    
    def generate_order(self) -> Order:
        """Generate a random order"""
        self.order_count += 1
        side = random.choice(["buy", "sell"])
        is_market = random.random() < self.market_order_ratio
        order_type = "market" if is_market else "limit"
        
        order = Order(
            client_id=f"load_test_{self.order_count}",
            instrument=self.instrument,
            side=side,
            type=order_type,
            quantity=self.generate_quantity(),
        )
        
        if order_type == "limit":
            order.price = self.generate_price(side)
        
        return order


class LoadTester:
    """Async load tester"""
    
    def __init__(self, api_url: str, generator: OrderGenerator):
        self.api_url = api_url
        self.generator = generator
        self.results = LoadTestResults()
        self.session = None
    
    async def submit_order(self, order: Order) -> RequestResult:
        """Submit a single order"""
        url = f"{self.api_url}/orders"
        payload = order.to_dict()
        
        start_time = time.time()
        
        try:
            async with self.session.post(url, json=payload, timeout=aiohttp.ClientTimeout(total=30)) as response:
                latency_ms = (time.time() - start_time) * 1000
                
                success = 200 <= response.status < 300
                error = None if success else await response.text()
                
                return RequestResult(
                    status_code=response.status,
                    latency_ms=latency_ms,
                    success=success,
                    error=error,
                )
        
        except asyncio.TimeoutError:
            latency_ms = (time.time() - start_time) * 1000
            return RequestResult(
                status_code=0,
                latency_ms=latency_ms,
                success=False,
                error="Timeout",
            )
        
        except Exception as e:
            latency_ms = (time.time() - start_time) * 1000
            return RequestResult(
                status_code=0,
                latency_ms=latency_ms,
                success=False,
                error=str(e),
            )
    
    async def worker(self, worker_id: int, duration: float, target_rate_per_worker: float):
        """Worker coroutine that submits orders"""
        end_time = time.time() + duration
        request_count = 0
        
        while time.time() < end_time:
            # Generate and submit order
            order = self.generator.generate_order()
            result = await self.submit_order(order)
            self.results.add_result(result)
            
            request_count += 1
            
            # Progress update (every 100 requests)
            if request_count % 100 == 0:
                elapsed = time.time() - self.results.start_time
                current_rate = self.results.total_requests / elapsed if elapsed > 0 else 0
                print(f"  Worker {worker_id}: {request_count} requests, {current_rate:.0f} req/sec", end="\r")
            
            # Sleep to maintain target rate
            if target_rate_per_worker > 0:
                sleep_time = 1.0 / target_rate_per_worker
                await asyncio.sleep(sleep_time)
    
    async def run_test(self, workers: int, duration: float, target_rate: float):
        """Run load test with multiple workers"""
        print(f"\nStarting load test...")
        print(f"   Workers: {workers}")
        print(f"   Duration: {duration} seconds")
        print(f"   Target Rate: {target_rate} orders/sec")
        print(f"   API URL: {self.api_url}")
        print()
        
        # Create aiohttp session
        connector = aiohttp.TCPConnector(limit=workers * 2)
        self.session = aiohttp.ClientSession(connector=connector)
        
        try:
            # Start time
            self.results.start_time = time.time()
            
            # Calculate rate per worker
            target_rate_per_worker = target_rate / workers if target_rate > 0 else 0
            
            # Create worker tasks
            tasks = [
                self.worker(i, duration, target_rate_per_worker)
                for i in range(workers)
            ]
            
            # Run all workers concurrently
            await asyncio.gather(*tasks)
            
            # End time
            self.results.end_time = time.time()
        
        finally:
            await self.session.close()
        
        print("\n\nLoad test complete")


async def main():
    parser = argparse.ArgumentParser(
        description="Simple load tester for Trading Engine (no k6 required)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    
    # API configuration
    parser.add_argument(
        "--api-url",
        type=str,
        default="http://localhost:8080",
        help="Trading Engine API URL (default: http://localhost:8080)",
    )
    
    # Load test parameters
    parser.add_argument(
        "--workers",
        type=int,
        default=50,
        help="Number of concurrent workers (default: 50)",
    )
    parser.add_argument(
        "--duration",
        type=int,
        default=30,
        help="Test duration in seconds (default: 30)",
    )
    parser.add_argument(
        "--target-rate",
        type=int,
        default=2000,
        help="Target orders per second (default: 2000, 0=unlimited)",
    )
    
    # Order generation parameters
    parser.add_argument(
        "--instrument",
        type=str,
        default="BTC-USD",
        help="Trading instrument (default: BTC-USD)",
    )
    parser.add_argument(
        "--min-price",
        type=float,
        default=50000.0,
        help="Minimum price (default: 50000.0)",
    )
    parser.add_argument(
        "--max-price",
        type=float,
        default=51000.0,
        help="Maximum price (default: 51000.0)",
    )
    parser.add_argument(
        "--spread",
        type=float,
        default=0.0005,
        help="Bid-ask spread (default: 0.0005 = 0.05%%)",
    )
    parser.add_argument(
        "--market-order-ratio",
        type=float,
        default=0.05,
        help="Ratio of market orders (default: 0.05 = 5%%)",
    )
    
    # Burst mode
    parser.add_argument(
        "--burst-mode",
        action="store_true",
        help="Run burst test (sudden spike in load)",
    )
    parser.add_argument(
        "--burst-workers",
        type=int,
        default=200,
        help="Number of workers during burst (default: 200)",
    )
    parser.add_argument(
        "--burst-duration",
        type=int,
        default=10,
        help="Burst duration in seconds (default: 10)",
    )
    
    # Output
    parser.add_argument(
        "--save-results",
        type=str,
        help="Save results to JSON file",
    )
    
    args = parser.parse_args()
    
    # Print configuration
    print("="*60)
    print("LOAD TEST CONFIGURATION")
    print("="*60)
    print(f"API URL: {args.api_url}")
    print(f"Instrument: {args.instrument}")
    print(f"Price Range: {args.min_price} - {args.max_price}")
    print(f"Market Order Ratio: {args.market_order_ratio * 100}%")
    print("="*60)
    
    # Create generator
    generator = OrderGenerator(
        instrument=args.instrument,
        min_price=args.min_price,
        max_price=args.max_price,
        spread=args.spread,
        market_order_ratio=args.market_order_ratio,
    )
    
    # Create tester
    tester = LoadTester(args.api_url, generator)
    
    # Run test
    if args.burst_mode:
        # Burst test: normal → spike → normal
        print("\n🔥 BURST TEST MODE")
        print(f"   Normal load: {args.workers} workers for {args.duration}s")
        print(f"   Burst load: {args.burst_workers} workers for {args.burst_duration}s")
        
        # Phase 1: Normal load
        print("\n>Phase 1: Normal Load")
        await tester.run_test(args.workers, args.duration, args.target_rate)
        normal_results = tester.results
        
        # Phase 2: Burst load
        print("\n>Phase 2: Burst Load")
        tester.results = LoadTestResults()
        await tester.run_test(args.burst_workers, args.burst_duration, args.target_rate * 2)
        burst_results = tester.results
        
        # Phase 3: Recovery
        print("\n>Phase 3: Recovery (Normal Load)")
        tester.results = LoadTestResults()
        await tester.run_test(args.workers, args.duration, args.target_rate)
        recovery_results = tester.results
        
        # Print all results
        print("\n" + "="*60)
        print("PHASE 1: NORMAL LOAD")
        normal_results.print_summary()
        
        print("\n" + "="*60)
        print("PHASE 2: BURST LOAD")
        burst_results.print_summary()
        
        print("\n" + "="*60)
        print("PHASE 3: RECOVERY")
        recovery_results.print_summary()
    
    else:
        # Regular sustained load test
        await tester.run_test(args.workers, args.duration, args.target_rate)
        tester.results.print_summary()
        
        # Save results if requested
        if args.save_results:
            summary = tester.results.get_summary()
            with open(args.save_results, "w") as f:
                json.dump(summary, f, indent=2)
            print(f"💾 Results saved to: {args.save_results}")


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\n\n Test interrupted by user")
        sys.exit(1)
