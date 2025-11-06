// Package cache provides cache metrics and monitoring
package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// CacheMetrics tracks cache performance metrics
type CacheMetrics struct {
	// Hit/Miss counters
	hits   atomic.Int64
	misses atomic.Int64

	// Invalidation counters
	invalidations      atomic.Int64
	invalidationErrors atomic.Int64

	// Latency tracking
	latencySum   atomic.Int64 // Sum of all latencies in microseconds
	latencyCount atomic.Int64

	// Stale data tracking
	staleHits atomic.Int64 // Cache hits that returned stale data

	// Per-symbol metrics
	symbolMetrics sync.Map // map[string]*SymbolMetrics

	// Start time for uptime calculation
	startTime time.Time
}

// SymbolMetrics tracks metrics for a specific symbol
type SymbolMetrics struct {
	Symbol        string
	Hits          atomic.Int64
	Misses        atomic.Int64
	Invalidations atomic.Int64
	LastAccess    time.Time
	mu            sync.RWMutex
}

func NewCacheMetrics() *CacheMetrics {
	return &CacheMetrics{
		startTime: time.Now(),
	}
}

func (cm *CacheMetrics) RecordHit(symbol string, latency time.Duration) {
	cm.hits.Add(1)
	cm.latencySum.Add(latency.Microseconds())
	cm.latencyCount.Add(1)

	cm.getOrCreateSymbolMetrics(symbol).RecordHit()
}

func (cm *CacheMetrics) RecordMiss(symbol string) {
	cm.misses.Add(1)
	cm.getOrCreateSymbolMetrics(symbol).RecordMiss()
}

func (cm *CacheMetrics) RecordStaleHit(symbol string) {
	cm.staleHits.Add(1)
	cm.hits.Add(1)
}

func (cm *CacheMetrics) RecordInvalidation(symbol string) {
	cm.invalidations.Add(1)
	cm.getOrCreateSymbolMetrics(symbol).RecordInvalidation()
}

func (cm *CacheMetrics) RecordInvalidationError() {
	cm.invalidationErrors.Add(1)
}

func (cm *CacheMetrics) GetHitRatio() float64 {
	hits := cm.hits.Load()
	misses := cm.misses.Load()
	total := hits + misses

	if total == 0 {
		return 0.0
	}

	return float64(hits) / float64(total)
}

func (cm *CacheMetrics) GetStaleRatio() float64 {
	staleHits := cm.staleHits.Load()
	totalHits := cm.hits.Load()

	if totalHits == 0 {
		return 0.0
	}

	return float64(staleHits) / float64(totalHits)
}

// GetAverageLatency returns average cache operation latency
func (cm *CacheMetrics) GetAverageLatency() time.Duration {
	count := cm.latencyCount.Load()
	if count == 0 {
		return 0
	}

	sum := cm.latencySum.Load()
	avgMicros := sum / count
	return time.Duration(avgMicros) * time.Microsecond
}

// GetStats returns current cache statistics
func (cm *CacheMetrics) GetStats() *CacheStats {
	hits := cm.hits.Load()
	misses := cm.misses.Load()
	total := hits + misses

	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hits) / float64(total)
	}

	stats := &CacheStats{
		Hits:                 hits,
		Misses:               misses,
		Total:                total,
		HitRatio:             hitRatio,
		HitRatioPercent:      hitRatio * 100,
		Invalidations:        cm.invalidations.Load(),
		InvalidationErrors:   cm.invalidationErrors.Load(),
		StaleHits:            cm.staleHits.Load(),
		StaleRatio:           cm.GetStaleRatio(),
		StaleRatioPercent:    cm.GetStaleRatio() * 100,
		AverageLatencyMicros: cm.GetAverageLatency().Microseconds(),
		UptimeSeconds:        int64(time.Since(cm.startTime).Seconds()),
	}

	// Add per-symbol stats
	stats.SymbolStats = make(map[string]*SymbolStats)
	cm.symbolMetrics.Range(func(key, value interface{}) bool {
		symbol := key.(string)
		metrics := value.(*SymbolMetrics)
		stats.SymbolStats[symbol] = metrics.GetStats()
		return true
	})

	return stats
}

// Reset resets all metrics
func (cm *CacheMetrics) Reset() {
	cm.hits.Store(0)
	cm.misses.Store(0)
	cm.invalidations.Store(0)
	cm.invalidationErrors.Store(0)
	cm.staleHits.Store(0)
	cm.latencySum.Store(0)
	cm.latencyCount.Store(0)
	cm.symbolMetrics = sync.Map{}
	cm.startTime = time.Now()
}

// getOrCreateSymbolMetrics gets or creates metrics for a symbol
func (cm *CacheMetrics) getOrCreateSymbolMetrics(symbol string) *SymbolMetrics {
	if value, ok := cm.symbolMetrics.Load(symbol); ok {
		return value.(*SymbolMetrics)
	}

	metrics := &SymbolMetrics{
		Symbol:     symbol,
		LastAccess: time.Now(),
	}

	actual, _ := cm.symbolMetrics.LoadOrStore(symbol, metrics)
	return actual.(*SymbolMetrics)
}

// RecordHit records a hit for this symbol
func (sm *SymbolMetrics) RecordHit() {
	sm.Hits.Add(1)
	sm.mu.Lock()
	sm.LastAccess = time.Now()
	sm.mu.Unlock()
}

// RecordMiss records a miss for this symbol
func (sm *SymbolMetrics) RecordMiss() {
	sm.Misses.Add(1)
	sm.mu.Lock()
	sm.LastAccess = time.Now()
	sm.mu.Unlock()
}

// RecordInvalidation records an invalidation for this symbol
func (sm *SymbolMetrics) RecordInvalidation() {
	sm.Invalidations.Add(1)
}

// GetStats returns statistics for this symbol
func (sm *SymbolMetrics) GetStats() *SymbolStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	hits := sm.Hits.Load()
	misses := sm.Misses.Load()
	total := hits + misses

	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hits) / float64(total)
	}

	return &SymbolStats{
		Symbol:          sm.Symbol,
		Hits:            hits,
		Misses:          misses,
		Total:           total,
		HitRatio:        hitRatio,
		HitRatioPercent: hitRatio * 100,
		Invalidations:   sm.Invalidations.Load(),
		LastAccess:      sm.LastAccess,
	}
}

// CacheStats represents overall cache statistics
type CacheStats struct {
	Hits                 int64                   `json:"hits"`
	Misses               int64                   `json:"misses"`
	Total                int64                   `json:"total"`
	HitRatio             float64                 `json:"hit_ratio"`
	HitRatioPercent      float64                 `json:"hit_ratio_percent"`
	Invalidations        int64                   `json:"invalidations"`
	InvalidationErrors   int64                   `json:"invalidation_errors"`
	StaleHits            int64                   `json:"stale_hits"`
	StaleRatio           float64                 `json:"stale_ratio"`
	StaleRatioPercent    float64                 `json:"stale_ratio_percent"`
	AverageLatencyMicros int64                   `json:"average_latency_micros"`
	UptimeSeconds        int64                   `json:"uptime_seconds"`
	SymbolStats          map[string]*SymbolStats `json:"symbol_stats,omitempty"`
}

// SymbolStats represents per-symbol cache statistics
type SymbolStats struct {
	Symbol          string    `json:"symbol"`
	Hits            int64     `json:"hits"`
	Misses          int64     `json:"misses"`
	Total           int64     `json:"total"`
	HitRatio        float64   `json:"hit_ratio"`
	HitRatioPercent float64   `json:"hit_ratio_percent"`
	Invalidations   int64     `json:"invalidations"`
	LastAccess      time.Time `json:"last_access"`
}

// MetricsCollector periodically collects and reports metrics
type MetricsCollector struct {
	metrics  *CacheMetrics
	interval time.Duration
	stopChan chan struct{}
	doneChan chan struct{}
	callback func(*CacheStats)
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(metrics *CacheMetrics, interval time.Duration, callback func(*CacheStats)) *MetricsCollector {
	return &MetricsCollector{
		metrics:  metrics,
		interval: interval,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
		callback: callback,
	}
}

// Start starts the metrics collector
func (mc *MetricsCollector) Start() {
	go mc.run()
}

// Stop stops the metrics collector
func (mc *MetricsCollector) Stop() {
	close(mc.stopChan)
	<-mc.doneChan
}

// run is the main metrics collection loop
func (mc *MetricsCollector) run() {
	defer close(mc.doneChan)

	ticker := time.NewTicker(mc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-mc.stopChan:
			return
		case <-ticker.C:
			stats := mc.metrics.GetStats()
			if mc.callback != nil {
				mc.callback(stats)
			}
		}
	}
}

// CacheHealthChecker monitors cache health and alerts on issues
type CacheHealthChecker struct {
	metrics          *CacheMetrics
	minHitRatio      float64
	maxStaleRatio    float64
	maxLatencyMicros int64
	alertCallback    func(string)
}

// NewCacheHealthChecker creates a new health checker
func NewCacheHealthChecker(metrics *CacheMetrics, alertCallback func(string)) *CacheHealthChecker {
	return &CacheHealthChecker{
		metrics:          metrics,
		minHitRatio:      0.70, // Alert if hit ratio < 70%
		maxStaleRatio:    0.10, // Alert if stale ratio > 10%
		maxLatencyMicros: 5000, // Alert if avg latency > 5ms
		alertCallback:    alertCallback,
	}
}

// SetThresholds sets health check thresholds
func (chc *CacheHealthChecker) SetThresholds(minHitRatio, maxStaleRatio float64, maxLatencyMicros int64) {
	chc.minHitRatio = minHitRatio
	chc.maxStaleRatio = maxStaleRatio
	chc.maxLatencyMicros = maxLatencyMicros
}

// Check performs health checks and returns issues
func (chc *CacheHealthChecker) Check() []string {
	stats := chc.metrics.GetStats()
	var issues []string

	// Check hit ratio
	if stats.HitRatio < chc.minHitRatio && stats.Total > 100 {
		issue := "Low cache hit ratio: %.1f%% (threshold: %.1f%%)"
		issues = append(issues, formatIssue(issue, stats.HitRatioPercent, chc.minHitRatio*100))
		if chc.alertCallback != nil {
			chc.alertCallback(formatIssue(issue, stats.HitRatioPercent, chc.minHitRatio*100))
		}
	}

	// Check stale ratio
	if stats.StaleRatio > chc.maxStaleRatio && stats.Hits > 100 {
		issue := "High stale data ratio: %.1f%% (threshold: %.1f%%) - Consider shortening TTL"
		issues = append(issues, formatIssue(issue, stats.StaleRatioPercent, chc.maxStaleRatio*100))
		if chc.alertCallback != nil {
			chc.alertCallback(formatIssue(issue, stats.StaleRatioPercent, chc.maxStaleRatio*100))
		}
	}

	// Check latency
	if stats.AverageLatencyMicros > chc.maxLatencyMicros && stats.Total > 100 {
		issue := "High cache latency: %dµs (threshold: %dµs)"
		issues = append(issues, formatIssue(issue, stats.AverageLatencyMicros, chc.maxLatencyMicros))
		if chc.alertCallback != nil {
			chc.alertCallback(formatIssue(issue, stats.AverageLatencyMicros, chc.maxLatencyMicros))
		}
	}

	// Check invalidation errors
	if stats.InvalidationErrors > 0 {
		issue := "Cache invalidation errors detected: %d"
		issues = append(issues, formatIssue(issue, stats.InvalidationErrors))
		if chc.alertCallback != nil {
			chc.alertCallback(formatIssue(issue, stats.InvalidationErrors))
		}
	}

	return issues
}

// formatIssue formats an issue message
func formatIssue(format string, args ...interface{}) string {
	// Simple sprintf-like formatting
	result := format
	for _, arg := range args {
		switch v := arg.(type) {
		case float64:
			result = replaceFirst(result, "%.1f", formatFloat(v, 1))
		case int64:
			result = replaceFirst(result, "%d", formatInt(v))
		}
	}
	return result
}

// Helper functions for formatting
func replaceFirst(s, old, new string) string {
	for i := 0; i < len(s); i++ {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}

func formatFloat(f float64, decimals int) string {
	// Simple float formatting
	s := ""
	if f < 0 {
		s = "-"
		f = -f
	}

	// Integer part
	intPart := int64(f)
	s += formatInt(intPart)

	// Decimal part
	if decimals > 0 {
		s += "."
		decPart := f - float64(intPart)
		for i := 0; i < decimals; i++ {
			decPart *= 10
			digit := int64(decPart) % 10
			s += formatInt(digit)
		}
	}

	return s
}

func formatInt(i int64) string {
	if i == 0 {
		return "0"
	}

	s := ""
	if i < 0 {
		s = "-"
		i = -i
	}

	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}

	return s
}

// Example integration with OrderbookCache
/*
// Add to OrderbookCache struct:
type OrderbookCache struct {
    redis      *RedisCache
    keyBuilder *KeyBuilder
    defaultTTL time.Duration
    metrics    *CacheMetrics  // ← Add this
}

// Modify GetOrderbook to track metrics:
func (oc *OrderbookCache) GetOrderbook(symbol string) (*OrderbookSnapshot, error) {
    start := time.Now()
    key := oc.keyBuilder.OrderbookKey(symbol)

    var snapshot OrderbookSnapshot
    err := oc.redis.GetJSON(key, &snapshot)

    latency := time.Since(start)

    if err != nil {
        oc.metrics.RecordMiss(symbol)
        return nil, fmt.Errorf("orderbook cache miss for %s: %w", symbol, err)
    }

    // Check if data is stale (optional)
    age := time.Since(snapshot.Timestamp)
    if age > oc.defaultTTL {
        oc.metrics.RecordStaleHit(symbol)
    } else {
        oc.metrics.RecordHit(symbol, latency)
    }

    return &snapshot, nil
}

// Modify InvalidateOrderbook to track metrics:
func (oc *OrderbookCache) InvalidateOrderbook(symbol string) error {
    keys := []string{
        oc.keyBuilder.OrderbookKey(symbol),
        oc.keyBuilder.OrderbookTopKey(symbol),
    }

    for _, key := range keys {
        if err := oc.redis.Delete(key); err != nil {
            oc.metrics.RecordInvalidationError()
            fmt.Printf("Warning: failed to invalidate cache key %s: %v\n", key, err)
        } else {
            oc.metrics.RecordInvalidation(symbol)
        }
    }

    return nil
}
*/
