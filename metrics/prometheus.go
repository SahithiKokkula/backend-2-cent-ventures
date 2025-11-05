package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Counter: Total orders received
	OrdersReceivedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orders_received_total",
			Help: "Total number of orders received by the trading engine",
		},
		[]string{"instrument", "side", "type"}, // Labels: instrument, buy/sell, limit/market
	)

	// Counter: Total orders matched
	OrdersMatchedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orders_matched_total",
			Help: "Total number of orders successfully matched",
		},
		[]string{"instrument", "side"}, // Labels: instrument, buy/sell
	)

	// Counter: Total orders rejected
	OrdersRejectedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orders_rejected_total",
			Help: "Total number of orders rejected due to validation or other errors",
		},
		[]string{"instrument", "reason"}, // Labels: instrument, rejection reason
	)

	// Histogram: Order processing latency
	OrderLatencySeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "order_latency_seconds",
			Help:    "Time taken to process an order from receipt to execution",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 15), // 0.1ms to ~3.2s
		},
		[]string{"instrument", "type"}, // Labels: instrument, limit/market
	)

	// Gauge: Current orderbook depth (total orders)
	CurrentOrderbookDepth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "current_orderbook_depth",
			Help: "Current number of orders in the orderbook",
		},
		[]string{"instrument", "side"}, // Labels: instrument, buy/sell
	)

	// Additional useful metrics
	
	// Gauge: Best bid/ask prices
	BestBidPrice = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "best_bid_price",
			Help: "Current best bid price in the orderbook",
		},
		[]string{"instrument"},
	)

	BestAskPrice = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "best_ask_price",
			Help: "Current best ask price in the orderbook",
		},
		[]string{"instrument"},
	)

	// Counter: Total trades executed
	TradesExecutedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "trades_executed_total",
			Help: "Total number of trades executed",
		},
		[]string{"instrument"},
	)

	// Counter: Total volume traded
	TradedVolumeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "traded_volume_total",
			Help: "Total volume traded",
		},
		[]string{"instrument"},
	)

	// Gauge: Spread (difference between best ask and best bid)
	OrderbookSpread = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "orderbook_spread",
			Help: "Current spread between best bid and best ask",
		},
		[]string{"instrument"},
	)

	// Histogram: Trade size distribution
	TradeSizeDistribution = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "trade_size_distribution",
			Help:    "Distribution of trade sizes",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 0.01 to ~40
		},
		[]string{"instrument"},
	)
)

// RecordOrderReceived increments the orders_received_total counter
func RecordOrderReceived(instrument, side, orderType string) {
	OrdersReceivedTotal.WithLabelValues(instrument, side, orderType).Inc()
}

// RecordOrderMatched increments the orders_matched_total counter
func RecordOrderMatched(instrument, side string) {
	OrdersMatchedTotal.WithLabelValues(instrument, side).Inc()
}

// RecordOrderRejected increments the orders_rejected_total counter
func RecordOrderRejected(instrument, reason string) {
	OrdersRejectedTotal.WithLabelValues(instrument, reason).Inc()
}

// RecordOrderLatency records the time taken to process an order
func RecordOrderLatency(instrument, orderType string, seconds float64) {
	OrderLatencySeconds.WithLabelValues(instrument, orderType).Observe(seconds)
}

// UpdateOrderbookDepth updates the current orderbook depth gauge
func UpdateOrderbookDepth(instrument, side string, depth float64) {
	CurrentOrderbookDepth.WithLabelValues(instrument, side).Set(depth)
}

// UpdateBestPrices updates best bid/ask prices
func UpdateBestPrices(instrument string, bestBid, bestAsk float64) {
	if bestBid > 0 {
		BestBidPrice.WithLabelValues(instrument).Set(bestBid)
	}
	if bestAsk > 0 {
		BestAskPrice.WithLabelValues(instrument).Set(bestAsk)
	}
	if bestBid > 0 && bestAsk > 0 {
		OrderbookSpread.WithLabelValues(instrument).Set(bestAsk - bestBid)
	}
}

// RecordTrade records a trade execution
func RecordTrade(instrument string, quantity float64) {
	TradesExecutedTotal.WithLabelValues(instrument).Inc()
	TradedVolumeTotal.WithLabelValues(instrument).Add(quantity)
	TradeSizeDistribution.WithLabelValues(instrument).Observe(quantity)
}
