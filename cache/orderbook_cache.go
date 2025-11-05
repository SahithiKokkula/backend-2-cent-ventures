package cache

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

type OrderbookCache struct {
	redis      *RedisCache
	keyBuilder *KeyBuilder
	defaultTTL time.Duration
}

type OrderbookCacheConfig struct {
	DefaultTTL   time.Duration
	TopOfBookTTL time.Duration
}

func DefaultOrderbookCacheConfig() *OrderbookCacheConfig {
	return &OrderbookCacheConfig{
		DefaultTTL:   5 * time.Second,
		TopOfBookTTL: 2 * time.Second,
	}
}

func NewOrderbookCache(redis *RedisCache, config *OrderbookCacheConfig) *OrderbookCache {
	if config == nil {
		config = DefaultOrderbookCacheConfig()
	}

	return &OrderbookCache{
		redis:      redis,
		keyBuilder: NewKeyBuilder("trading"),
		defaultTTL: config.DefaultTTL,
	}
}

type OrderbookSnapshot struct {
	Symbol    string       `json:"symbol"`
	Bids      []PriceLevel `json:"bids"`
	Asks      []PriceLevel `json:"asks"`
	Timestamp time.Time    `json:"timestamp"`
	Sequence  int64        `json:"sequence,omitempty"`
}

type PriceLevel struct {
	Price    decimal.Decimal `json:"price"`
	Quantity decimal.Decimal `json:"quantity"`
	Orders   int             `json:"orders,omitempty"`
}

type TopOfBook struct {
	Symbol    string          `json:"symbol"`
	BestBid   *PriceLevel     `json:"best_bid,omitempty"`
	BestAsk   *PriceLevel     `json:"best_ask,omitempty"`
	Spread    decimal.Decimal `json:"spread"`
	Timestamp time.Time       `json:"timestamp"`
}

func (oc *OrderbookCache) GetOrderbook(symbol string) (*OrderbookSnapshot, error) {
	key := oc.keyBuilder.OrderbookKey(symbol)

	var snapshot OrderbookSnapshot
	err := oc.redis.GetJSON(key, &snapshot)
	if err != nil {
		return nil, fmt.Errorf("orderbook cache miss for %s: %w", symbol, err)
	}

	return &snapshot, nil
}

func (oc *OrderbookCache) SetOrderbook(snapshot *OrderbookSnapshot, ttl time.Duration) error {
	if ttl == 0 {
		ttl = oc.defaultTTL
	}

	snapshot.Timestamp = time.Now()
	key := oc.keyBuilder.OrderbookKey(snapshot.Symbol)

	err := oc.redis.SetJSON(key, snapshot, ttl)
	if err != nil {
		return fmt.Errorf("failed to cache orderbook for %s: %w", snapshot.Symbol, err)
	}

	return nil
}

// GetTopOfBook retrieves top-of-book from cache
func (oc *OrderbookCache) GetTopOfBook(symbol string) (*TopOfBook, error) {
	key := oc.keyBuilder.OrderbookTopKey(symbol)

	var top TopOfBook
	err := oc.redis.GetJSON(key, &top)
	if err != nil {
		return nil, fmt.Errorf("top-of-book cache miss for %s: %w", symbol, err)
	}

	return &top, nil
}

// SetTopOfBook stores top-of-book in cache
func (oc *OrderbookCache) SetTopOfBook(top *TopOfBook, ttl time.Duration) error {
	if ttl == 0 {
		ttl = 2 * time.Second // Shorter TTL for top of book
	}

	top.Timestamp = time.Now()
	key := oc.keyBuilder.OrderbookTopKey(top.Symbol)

	err := oc.redis.SetJSON(key, top, ttl)
	if err != nil {
		return fmt.Errorf("failed to cache top-of-book for %s: %w", top.Symbol, err)
	}

	return nil
}

// InvalidateOrderbook removes orderbook cache for a symbol
func (oc *OrderbookCache) InvalidateOrderbook(symbol string) error {
	// Delete both full orderbook and top-of-book
	keys := []string{
		oc.keyBuilder.OrderbookKey(symbol),
		oc.keyBuilder.OrderbookTopKey(symbol),
	}

	for _, key := range keys {
		if err := oc.redis.Delete(key); err != nil {
			// Log error but don't fail - cache invalidation is best-effort
			fmt.Printf("Warning: failed to invalidate cache key %s: %v\n", key, err)
		}
	}

	return nil
}

// InvalidateAllOrderbooks removes all orderbook caches
func (oc *OrderbookCache) InvalidateAllOrderbooks() error {
	patterns := []string{
		oc.keyBuilder.OrderbookKey("*"),
		oc.keyBuilder.OrderbookTopKey("*"),
	}

	for _, pattern := range patterns {
		if err := oc.redis.DeletePattern(pattern); err != nil {
			return fmt.Errorf("failed to invalidate orderbook caches: %w", err)
		}
	}

	return nil
}

// GetOrderbookDepth retrieves limited depth orderbook from cache
func (oc *OrderbookCache) GetOrderbookDepth(symbol string, depth int) (*OrderbookSnapshot, error) {
	// Try to get full orderbook and truncate
	snapshot, err := oc.GetOrderbook(symbol)
	if err != nil {
		return nil, err
	}

	// Truncate to requested depth
	if len(snapshot.Bids) > depth {
		snapshot.Bids = snapshot.Bids[:depth]
	}
	if len(snapshot.Asks) > depth {
		snapshot.Asks = snapshot.Asks[:depth]
	}

	return snapshot, nil
}

// CacheOrderbookWithDepth stores both full and limited depth versions
func (oc *OrderbookCache) CacheOrderbookWithDepth(snapshot *OrderbookSnapshot, depths []int, ttl time.Duration) error {
	// Cache full orderbook
	if err := oc.SetOrderbook(snapshot, ttl); err != nil {
		return err
	}

	// Extract and cache top of book
	top := ExtractTopOfBook(snapshot)
	if err := oc.SetTopOfBook(top, ttl/2); err != nil {
		// Log but don't fail
		fmt.Printf("Warning: failed to cache top-of-book: %v\n", err)
	}

	return nil
}

// ExtractTopOfBook extracts top-of-book from orderbook snapshot
func ExtractTopOfBook(snapshot *OrderbookSnapshot) *TopOfBook {
	top := &TopOfBook{
		Symbol:    snapshot.Symbol,
		Timestamp: snapshot.Timestamp,
	}

	if len(snapshot.Bids) > 0 {
		top.BestBid = &snapshot.Bids[0]
	}

	if len(snapshot.Asks) > 0 {
		top.BestAsk = &snapshot.Asks[0]
	}

	// Calculate spread
	if top.BestBid != nil && top.BestAsk != nil {
		top.Spread = top.BestAsk.Price.Sub(top.BestBid.Price)
	}

	return top
}

// OrderbookStats returns statistics about orderbook caches
type OrderbookStats struct {
	Symbol          string        `json:"symbol"`
	FullBookCached  bool          `json:"full_book_cached"`
	TopOfBookCached bool          `json:"top_of_book_cached"`
	FullBookTTL     time.Duration `json:"full_book_ttl,omitempty"`
	TopOfBookTTL    time.Duration `json:"top_of_book_ttl,omitempty"`
	LastUpdate      time.Time     `json:"last_update,omitempty"`
}

// GetOrderbookStats returns caching statistics for a symbol
func (oc *OrderbookCache) GetOrderbookStats(symbol string) (*OrderbookStats, error) {
	stats := &OrderbookStats{
		Symbol: symbol,
	}

	// Check full orderbook
	fullBookKey := oc.keyBuilder.OrderbookKey(symbol)
	fullBookExists, err := oc.redis.Exists(fullBookKey)
	if err == nil && fullBookExists {
		stats.FullBookCached = true
		if ttl, err := oc.redis.GetTTL(fullBookKey); err == nil {
			stats.FullBookTTL = ttl
		}

		// Get timestamp from cached data
		if snapshot, err := oc.GetOrderbook(symbol); err == nil {
			stats.LastUpdate = snapshot.Timestamp
		}
	}

	// Check top of book
	topKey := oc.keyBuilder.OrderbookTopKey(symbol)
	topExists, err := oc.redis.Exists(topKey)
	if err == nil && topExists {
		stats.TopOfBookCached = true
		if ttl, err := oc.redis.GetTTL(topKey); err == nil {
			stats.TopOfBookTTL = ttl
		}
	}

	return stats, nil
}

// WarmCache pre-populates cache with orderbook data
func (oc *OrderbookCache) WarmCache(snapshots []*OrderbookSnapshot, ttl time.Duration) error {
	if ttl == 0 {
		ttl = oc.defaultTTL
	}

	for _, snapshot := range snapshots {
		if err := oc.SetOrderbook(snapshot, ttl); err != nil {
			return fmt.Errorf("failed to warm cache for %s: %w", snapshot.Symbol, err)
		}

		// Also cache top of book
		top := ExtractTopOfBook(snapshot)
		if err := oc.SetTopOfBook(top, ttl/2); err != nil {
			fmt.Printf("Warning: failed to warm top-of-book cache for %s: %v\n", snapshot.Symbol, err)
		}
	}

	return nil
}

// SerializeOrderbookCompact creates a compact JSON representation
func SerializeOrderbookCompact(snapshot *OrderbookSnapshot) ([]byte, error) {
	// Create compact representation (array format instead of verbose JSON)
	compact := map[string]interface{}{
		"s":  snapshot.Symbol,
		"b":  convertPriceLevelsToArrays(snapshot.Bids),
		"a":  convertPriceLevelsToArrays(snapshot.Asks),
		"t":  snapshot.Timestamp.Unix(),
		"sq": snapshot.Sequence,
	}

	return json.Marshal(compact)
}

// DeserializeOrderbookCompact parses a compact JSON representation
func DeserializeOrderbookCompact(data []byte) (*OrderbookSnapshot, error) {
	var compact map[string]interface{}
	if err := json.Unmarshal(data, &compact); err != nil {
		return nil, err
	}

	snapshot := &OrderbookSnapshot{
		Symbol:   compact["s"].(string),
		Sequence: int64(compact["sq"].(float64)),
	}

	// Parse timestamp
	if ts, ok := compact["t"].(float64); ok {
		snapshot.Timestamp = time.Unix(int64(ts), 0)
	}

	// Parse bids
	if bids, ok := compact["b"].([]interface{}); ok {
		snapshot.Bids = convertArraysToPriceLevels(bids)
	}

	// Parse asks
	if asks, ok := compact["a"].([]interface{}); ok {
		snapshot.Asks = convertArraysToPriceLevels(asks)
	}

	return snapshot, nil
}

// Helper functions for compact serialization
func convertPriceLevelsToArrays(levels []PriceLevel) [][]string {
	result := make([][]string, len(levels))
	for i, level := range levels {
		result[i] = []string{
			level.Price.String(),
			level.Quantity.String(),
		}
	}
	return result
}

func convertArraysToPriceLevels(arrays []interface{}) []PriceLevel {
	result := make([]PriceLevel, len(arrays))
	for i, arr := range arrays {
		if pricePair, ok := arr.([]interface{}); ok && len(pricePair) >= 2 {
			price, _ := decimal.NewFromString(pricePair[0].(string))
			qty, _ := decimal.NewFromString(pricePair[1].(string))
			result[i] = PriceLevel{
				Price:    price,
				Quantity: qty,
			}
		}
	}
	return result
}
