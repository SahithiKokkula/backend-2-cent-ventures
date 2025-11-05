package cache

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TradesCache struct {
	redis      *RedisCache
	keyBuilder *KeyBuilder
	defaultTTL time.Duration
}

type TradesCacheConfig struct {
	DefaultTTL        time.Duration
	MaxTradesPerCache int
}

func DefaultTradesCacheConfig() *TradesCacheConfig {
	return &TradesCacheConfig{
		DefaultTTL:        10 * time.Second,
		MaxTradesPerCache: 100,
	}
}

func NewTradesCache(redis *RedisCache, config *TradesCacheConfig) *TradesCache {
	if config == nil {
		config = DefaultTradesCacheConfig()
	}

	return &TradesCache{
		redis:      redis,
		keyBuilder: NewKeyBuilder("trading"),
		defaultTTL: config.DefaultTTL,
	}
}

type Trade struct {
	TradeID     uuid.UUID       `json:"trade_id"`
	Symbol      string          `json:"symbol"`
	BuyOrderID  uuid.UUID       `json:"buy_order_id"`
	SellOrderID uuid.UUID       `json:"sell_order_id"`
	Price       decimal.Decimal `json:"price"`
	Quantity    decimal.Decimal `json:"quantity"`
	Timestamp   time.Time       `json:"timestamp"`
	Side        string          `json:"side,omitempty"`
}

type TradesList struct {
	Symbol    string    `json:"symbol"`
	Trades    []Trade   `json:"trades"`
	Count     int       `json:"count"`
	Timestamp time.Time `json:"timestamp"`
}

func (tc *TradesCache) GetTrades(symbol string, limit int) (*TradesList, error) {
	key := tc.keyBuilder.TradesKey(symbol, limit)

	var tradesList TradesList
	err := tc.redis.GetJSON(key, &tradesList)
	if err != nil {
		return nil, fmt.Errorf("trades cache miss for %s (limit %d): %w", symbol, limit, err)
	}

	return &tradesList, nil
}

func (tc *TradesCache) SetTrades(tradesList *TradesList, limit int, ttl time.Duration) error {
	if ttl == 0 {
		ttl = tc.defaultTTL
	}

	tradesList.Timestamp = time.Now()
	tradesList.Count = len(tradesList.Trades)

	if len(tradesList.Trades) > limit {
		tradesList.Trades = tradesList.Trades[:limit]
		tradesList.Count = limit
	}

	key := tc.keyBuilder.TradesKey(tradesList.Symbol, limit)

	err := tc.redis.SetJSON(key, tradesList, ttl)
	if err != nil {
		return fmt.Errorf("failed to cache trades for %s: %w", tradesList.Symbol, err)
	}

	return nil
}

// GetLatestTrades retrieves the latest trades without limit from cache
func (tc *TradesCache) GetLatestTrades(symbol string) (*TradesList, error) {
	key := tc.keyBuilder.TradesLatestKey(symbol)

	var tradesList TradesList
	err := tc.redis.GetJSON(key, &tradesList)
	if err != nil {
		return nil, fmt.Errorf("latest trades cache miss for %s: %w", symbol, err)
	}

	return &tradesList, nil
}

// SetLatestTrades stores the latest trades (without specific limit)
func (tc *TradesCache) SetLatestTrades(tradesList *TradesList, ttl time.Duration) error {
	if ttl == 0 {
		ttl = tc.defaultTTL
	}

	tradesList.Timestamp = time.Now()
	tradesList.Count = len(tradesList.Trades)

	key := tc.keyBuilder.TradesLatestKey(tradesList.Symbol)

	err := tc.redis.SetJSON(key, tradesList, ttl)
	if err != nil {
		return fmt.Errorf("failed to cache latest trades for %s: %w", tradesList.Symbol, err)
	}

	return nil
}

// InvalidateTrades removes trades cache for a symbol
func (tc *TradesCache) InvalidateTrades(symbol string) error {
	// Delete all trades caches for this symbol (all limits)
	pattern := fmt.Sprintf("trading:trades:%s:*", symbol)

	if err := tc.redis.DeletePattern(pattern); err != nil {
		return fmt.Errorf("failed to invalidate trades caches for %s: %w", symbol, err)
	}

	return nil
}

// InvalidateAllTrades removes all trades caches
func (tc *TradesCache) InvalidateAllTrades() error {
	pattern := "trading:trades:*"

	if err := tc.redis.DeletePattern(pattern); err != nil {
		return fmt.Errorf("failed to invalidate all trades caches: %w", err)
	}

	return nil
}

// AppendTrade adds a new trade to cached trades lists (updates multiple limits)
func (tc *TradesCache) AppendTrade(trade *Trade, limits []int, ttl time.Duration) error {
	if ttl == 0 {
		ttl = tc.defaultTTL
	}

	// For each limit, get existing trades, prepend new trade, and save
	for _, limit := range limits {
		key := tc.keyBuilder.TradesKey(trade.Symbol, limit)

		// Try to get existing trades
		var tradesList TradesList
		err := tc.redis.GetJSON(key, &tradesList)
		if err != nil {
			// If cache miss, create new list
			tradesList = TradesList{
				Symbol: trade.Symbol,
				Trades: []Trade{},
			}
		}

		// Prepend new trade (most recent first)
		tradesList.Trades = append([]Trade{*trade}, tradesList.Trades...)

		// Truncate to limit
		if len(tradesList.Trades) > limit {
			tradesList.Trades = tradesList.Trades[:limit]
		}

		tradesList.Count = len(tradesList.Trades)
		tradesList.Timestamp = time.Now()

		// Save back to cache
		if err := tc.redis.SetJSON(key, tradesList, ttl); err != nil {
			return fmt.Errorf("failed to append trade to cache (limit %d): %w", limit, err)
		}
	}

	return nil
}

// GetTradeByID retrieves a specific trade by ID
func (tc *TradesCache) GetTradeByID(tradeID uuid.UUID) (*Trade, error) {
	key := tc.keyBuilder.OrderKey(tradeID.String())

	var trade Trade
	err := tc.redis.GetJSON(key, &trade)
	if err != nil {
		return nil, fmt.Errorf("trade cache miss for %s: %w", tradeID, err)
	}

	return &trade, nil
}

// SetTradeByID caches a specific trade by ID
func (tc *TradesCache) SetTradeByID(trade *Trade, ttl time.Duration) error {
	if ttl == 0 {
		ttl = tc.defaultTTL
	}

	key := tc.keyBuilder.OrderKey(trade.TradeID.String())

	err := tc.redis.SetJSON(key, trade, ttl)
	if err != nil {
		return fmt.Errorf("failed to cache trade %s: %w", trade.TradeID, err)
	}

	return nil
}

// TradesStats returns statistics about trades caches
type TradesStats struct {
	Symbol       string        `json:"symbol"`
	CachedLimits []int         `json:"cached_limits"`
	LatestCached bool          `json:"latest_cached"`
	LatestCount  int           `json:"latest_count,omitempty"`
	LatestTTL    time.Duration `json:"latest_ttl,omitempty"`
	LastUpdate   time.Time     `json:"last_update,omitempty"`
}

// GetTradesStats returns caching statistics for a symbol
func (tc *TradesCache) GetTradesStats(symbol string, checkLimits []int) (*TradesStats, error) {
	stats := &TradesStats{
		Symbol:       symbol,
		CachedLimits: []int{},
	}

	// Check each limit
	for _, limit := range checkLimits {
		key := tc.keyBuilder.TradesKey(symbol, limit)
		exists, err := tc.redis.Exists(key)
		if err == nil && exists {
			stats.CachedLimits = append(stats.CachedLimits, limit)
		}
	}

	// Check latest trades
	latestKey := tc.keyBuilder.TradesLatestKey(symbol)
	latestExists, err := tc.redis.Exists(latestKey)
	if err == nil && latestExists {
		stats.LatestCached = true

		if ttl, err := tc.redis.GetTTL(latestKey); err == nil {
			stats.LatestTTL = ttl
		}

		// Get count from cached data
		if tradesList, err := tc.GetLatestTrades(symbol); err == nil {
			stats.LatestCount = tradesList.Count
			stats.LastUpdate = tradesList.Timestamp
		}
	}

	return stats, nil
}

// WarmCache pre-populates cache with trades data
func (tc *TradesCache) WarmCache(tradesLists []*TradesList, limits []int, ttl time.Duration) error {
	if ttl == 0 {
		ttl = tc.defaultTTL
	}

	for _, tradesList := range tradesLists {
		// Cache for each limit
		for _, limit := range limits {
			if err := tc.SetTrades(tradesList, limit, ttl); err != nil {
				return fmt.Errorf("failed to warm cache for %s (limit %d): %w", tradesList.Symbol, limit, err)
			}
		}

		// Also cache as latest
		if err := tc.SetLatestTrades(tradesList, ttl); err != nil {
			fmt.Printf("Warning: failed to warm latest trades cache for %s: %v\n", tradesList.Symbol, err)
		}
	}

	return nil
}

// GetRecentTradesSince retrieves trades after a specific timestamp
func (tc *TradesCache) GetRecentTradesSince(symbol string, since time.Time, limit int) ([]Trade, error) {
	// Get cached trades
	tradesList, err := tc.GetLatestTrades(symbol)
	if err != nil {
		return nil, err
	}

	// Filter trades after timestamp
	var filtered []Trade
	for _, trade := range tradesList.Trades {
		if trade.Timestamp.After(since) {
			filtered = append(filtered, trade)
			if len(filtered) >= limit {
				break
			}
		}
	}

	return filtered, nil
}

// GetTradeVolume calculates total volume from cached trades
func (tc *TradesCache) GetTradeVolume(symbol string, duration time.Duration) (decimal.Decimal, error) {
	// Get cached trades
	tradesList, err := tc.GetLatestTrades(symbol)
	if err != nil {
		return decimal.Zero, err
	}

	// Calculate volume for trades within duration
	cutoff := time.Now().Add(-duration)
	volume := decimal.Zero

	for _, trade := range tradesList.Trades {
		if trade.Timestamp.After(cutoff) {
			volume = volume.Add(trade.Quantity)
		}
	}

	return volume, nil
}

// GetVWAP calculates volume-weighted average price from cached trades
func (tc *TradesCache) GetVWAP(symbol string, duration time.Duration) (decimal.Decimal, error) {
	// Get cached trades
	tradesList, err := tc.GetLatestTrades(symbol)
	if err != nil {
		return decimal.Zero, err
	}

	// Calculate VWAP for trades within duration
	cutoff := time.Now().Add(-duration)
	totalValue := decimal.Zero
	totalVolume := decimal.Zero

	for _, trade := range tradesList.Trades {
		if trade.Timestamp.After(cutoff) {
			value := trade.Price.Mul(trade.Quantity)
			totalValue = totalValue.Add(value)
			totalVolume = totalVolume.Add(trade.Quantity)
		}
	}

	if totalVolume.IsZero() {
		return decimal.Zero, nil
	}

	return totalValue.Div(totalVolume), nil
}

// BatchSetTrades caches multiple trade lists efficiently
func (tc *TradesCache) BatchSetTrades(tradesMap map[string]*TradesList, ttl time.Duration) error {
	if ttl == 0 {
		ttl = tc.defaultTTL
	}

	// Prepare batch set
	pairs := make(map[string]interface{})

	for symbol, tradesList := range tradesMap {
		key := tc.keyBuilder.TradesLatestKey(symbol)
		tradesList.Timestamp = time.Now()
		tradesList.Count = len(tradesList.Trades)
		pairs[key] = tradesList
	}

	// Batch set all at once
	if err := tc.redis.MSet(pairs); err != nil {
		return fmt.Errorf("failed to batch set trades: %w", err)
	}

	// Set TTLs individually (Redis doesn't support batch expire)
	for key := range pairs {
		if err := tc.redis.Expire(key, ttl); err != nil {
			fmt.Printf("Warning: failed to set TTL for %s: %v\n", key, err)
		}
	}

	return nil
}
