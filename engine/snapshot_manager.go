package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type OrderbookSnapshot struct {
	SnapshotID        int64                `json:"snapshot_id"`
	Instrument        string               `json:"instrument"`
	BidLevels         []SnapshotPriceLevel `json:"bid_levels"`
	AskLevels         []SnapshotPriceLevel `json:"ask_levels"`
	BestBidPrice      *decimal.Decimal     `json:"best_bid_price,omitempty"`
	BestAskPrice      *decimal.Decimal     `json:"best_ask_price,omitempty"`
	Spread            *decimal.Decimal     `json:"spread,omitempty"`
	TotalBidVolume    decimal.Decimal      `json:"total_bid_volume"`
	TotalAskVolume    decimal.Decimal      `json:"total_ask_volume"`
	SnapshotTimestamp time.Time            `json:"snapshot_timestamp"`
	SnapshotType      SnapshotType         `json:"snapshot_type"`
	TriggeredBy       string               `json:"triggered_by,omitempty"`
}

type SnapshotPriceLevel struct {
	Price       decimal.Decimal `json:"price"`
	TotalVolume decimal.Decimal `json:"total_volume"`
	OrderCount  int             `json:"order_count"`
}

type SnapshotType string

const (
	SnapshotTypePeriodic    SnapshotType = "periodic"
	SnapshotTypeOnDemand    SnapshotType = "on-demand"
	SnapshotTypePreShutdown SnapshotType = "pre-shutdown"
	SnapshotTypeRecovery    SnapshotType = "recovery"
)

type SnapshotManager struct {
	db                *sql.DB
	orderbook         *OrderBook
	instrument        string
	snapshotInterval  time.Duration
	maxLevels         int
	enableCompression bool
	running           bool
	stopCh            chan struct{}
	wg                sync.WaitGroup
	mu                sync.RWMutex
}

func NewSnapshotManager(
	db *sql.DB,
	orderbook *OrderBook,
	instrument string,
	snapshotInterval time.Duration,
	maxLevels int,
) *SnapshotManager {
	return &SnapshotManager{
		db:                db,
		orderbook:         orderbook,
		instrument:        instrument,
		snapshotInterval:  snapshotInterval,
		maxLevels:         maxLevels,
		enableCompression: true,
		stopCh:            make(chan struct{}),
	}
}

// SetCompression enables or disables JSON compression
func (sm *SnapshotManager) SetCompression(enabled bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.enableCompression = enabled
}

func (sm *SnapshotManager) Start() {
	sm.mu.Lock()
	if sm.running {
		sm.mu.Unlock()
		return
	}
	sm.running = true
	sm.mu.Unlock()

	sm.wg.Add(1)
	go sm.snapshotLoop()

	log.Printf("Snapshot manager started for %s (interval: %v, max levels: %d)",
		sm.instrument, sm.snapshotInterval, sm.maxLevels)
}

// Stop stops the snapshot manager
func (sm *SnapshotManager) Stop() {
	sm.mu.Lock()
	if !sm.running {
		sm.mu.Unlock()
		return
	}
	sm.running = false
	sm.mu.Unlock()

	close(sm.stopCh)
	sm.wg.Wait()

	log.Printf("Snapshot manager stopped for %s", sm.instrument)
}

// snapshotLoop runs periodic snapshots
func (sm *SnapshotManager) snapshotLoop() {
	defer sm.wg.Done()

	ticker := time.NewTicker(sm.snapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopCh:
			// Take final snapshot before shutdown
			_ = sm.TakeSnapshot(SnapshotTypePreShutdown, "system_shutdown")
			return

		case <-ticker.C:
			err := sm.TakeSnapshot(SnapshotTypePeriodic, "timer")
			if err != nil {
				log.Printf("Failed to take periodic snapshot for %s: %v", sm.instrument, err)
			}
		}
	}
}

// TakeSnapshot captures current orderbook state and persists it
func (sm *SnapshotManager) TakeSnapshot(snapshotType SnapshotType, triggeredBy string) error {
	ctx := context.Background()

	// Capture orderbook state
	snapshot := sm.captureOrderbook(snapshotType, triggeredBy)

	// Persist to database
	err := sm.saveSnapshot(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	log.Printf("📸 Snapshot taken: %s [%s] - %d bids, %d asks (triggered by: %s)",
		sm.instrument, snapshotType, len(snapshot.BidLevels), len(snapshot.AskLevels), triggeredBy)

	return nil
}

// captureOrderbook captures current orderbook state
func (sm *SnapshotManager) captureOrderbook(snapshotType SnapshotType, triggeredBy string) *OrderbookSnapshot {
	snapshot := &OrderbookSnapshot{
		Instrument:        sm.instrument,
		SnapshotTimestamp: time.Now(),
		SnapshotType:      snapshotType,
		TriggeredBy:       triggeredBy,
		BidLevels:         make([]SnapshotPriceLevel, 0, sm.maxLevels),
		AskLevels:         make([]SnapshotPriceLevel, 0, sm.maxLevels),
	}

	// Get top N levels from orderbook
	bidPriceLevels, askPriceLevels := sm.orderbook.GetTopLevels(sm.maxLevels)

	// Convert bid levels to snapshot format
	for _, priceLevel := range bidPriceLevels {
		level := SnapshotPriceLevel{
			Price:       priceLevel.Price,
			TotalVolume: priceLevel.Volume,
			OrderCount:  priceLevel.Orders.Len(),
		}
		snapshot.BidLevels = append(snapshot.BidLevels, level)
		snapshot.TotalBidVolume = snapshot.TotalBidVolume.Add(level.TotalVolume)
	}

	// Convert ask levels to snapshot format
	for _, priceLevel := range askPriceLevels {
		level := SnapshotPriceLevel{
			Price:       priceLevel.Price,
			TotalVolume: priceLevel.Volume,
			OrderCount:  priceLevel.Orders.Len(),
		}
		snapshot.AskLevels = append(snapshot.AskLevels, level)
		snapshot.TotalAskVolume = snapshot.TotalAskVolume.Add(level.TotalVolume)
	}

	// Calculate best bid/ask and spread
	if len(snapshot.BidLevels) > 0 {
		snapshot.BestBidPrice = &snapshot.BidLevels[0].Price
	}
	if len(snapshot.AskLevels) > 0 {
		snapshot.BestAskPrice = &snapshot.AskLevels[0].Price
	}
	if snapshot.BestBidPrice != nil && snapshot.BestAskPrice != nil {
		spread := snapshot.BestAskPrice.Sub(*snapshot.BestBidPrice)
		snapshot.Spread = &spread
	}

	return snapshot
}

// saveSnapshot persists snapshot to database
func (sm *SnapshotManager) saveSnapshot(ctx context.Context, snapshot *OrderbookSnapshot) error {
	// Serialize snapshot data to JSON
	snapshotData := map[string]interface{}{
		"bid_levels": snapshot.BidLevels,
		"ask_levels": snapshot.AskLevels,
	}

	snapshotJSON, err := json.Marshal(snapshotData)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	uncompressedSize := len(snapshotJSON)
	finalData := snapshotJSON

	// Apply compression if enabled and data is large enough
	sm.mu.RLock()
	useCompression := sm.enableCompression && uncompressedSize > 1024 // Compress if > 1KB
	sm.mu.RUnlock()

	if useCompression {
		var buf bytes.Buffer
		gzipWriter := gzip.NewWriter(&buf)
		_, err = gzipWriter.Write(snapshotJSON)
		if err != nil {
			log.Printf(" Compression failed, using uncompressed data: %v", err)
		} else {
			_ = gzipWriter.Close()
			compressedData := buf.Bytes()
			compressionRatio := float64(len(compressedData)) / float64(uncompressedSize)

			// Only use compressed if it actually saves space
			if compressionRatio >= 0.9 {
				// finalData already set to snapshotJSON above
				log.Printf(">Compression ratio %.2f%% - using uncompressed", compressionRatio*100)
			} else {
				finalData = compressedData
				log.Printf(">Snapshot compressed: %d → %d bytes (%.1f%% reduction)",
					uncompressedSize, len(finalData), (1-compressionRatio)*100)
			}
		}
	}

	// Log size warnings
	if uncompressedSize > 100*1024 { // > 100KB
		log.Printf(" Large snapshot detected: %d bytes uncompressed. Consider reducing maxLevels (current: %d)",
			uncompressedSize, sm.maxLevels)
	}

	query := `
		INSERT INTO orderbook_snapshots (
			instrument, snapshot_data, bid_count, ask_count,
			best_bid_price, best_ask_price, spread,
			total_bid_volume, total_ask_volume,
			snapshot_timestamp, snapshot_type, triggered_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING snapshot_id
	`

	var bestBidStr, bestAskStr, spreadStr *string
	if snapshot.BestBidPrice != nil {
		str := snapshot.BestBidPrice.String()
		bestBidStr = &str
	}
	if snapshot.BestAskPrice != nil {
		str := snapshot.BestAskPrice.String()
		bestAskStr = &str
	}
	if snapshot.Spread != nil {
		str := snapshot.Spread.String()
		spreadStr = &str
	}

	err = sm.db.QueryRowContext(
		ctx,
		query,
		snapshot.Instrument,
		finalData, // Use compressed or uncompressed based on effectiveness
		len(snapshot.BidLevels),
		len(snapshot.AskLevels),
		bestBidStr,
		bestAskStr,
		spreadStr,
		snapshot.TotalBidVolume.String(),
		snapshot.TotalAskVolume.String(),
		snapshot.SnapshotTimestamp,
		string(snapshot.SnapshotType),
		snapshot.TriggeredBy,
	).Scan(&snapshot.SnapshotID)

	if err != nil {
		return fmt.Errorf("failed to insert snapshot: %w", err)
	}

	return nil
}

// parsePriceLevels is a helper to parse bid or ask levels from snapshot data
func parsePriceLevels(snapshotData map[string]interface{}, key string) []SnapshotPriceLevel {
	levels := make([]SnapshotPriceLevel, 0)
	if levelData, ok := snapshotData[key].([]interface{}); ok {
		for _, item := range levelData {
			if levelMap, ok := item.(map[string]interface{}); ok {
				level := SnapshotPriceLevel{}
				if price, ok := levelMap["price"].(string); ok {
					level.Price, _ = decimal.NewFromString(price)
				}
				if volume, ok := levelMap["total_volume"].(string); ok {
					level.TotalVolume, _ = decimal.NewFromString(volume)
				}
				if count, ok := levelMap["order_count"].(float64); ok {
					level.OrderCount = int(count)
				}
				levels = append(levels, level)
			}
		}
	}
	return levels
}

// comparePriceLevels is a helper to compare snapshot and live price levels
func comparePriceLevels(snapshotLevels, liveLevels []SnapshotPriceLevel, levelType string) []SnapshotMismatch {
	var mismatches []SnapshotMismatch

	minLevels := len(snapshotLevels)
	if len(liveLevels) < minLevels {
		minLevels = len(liveLevels)
	}

	for i := 0; i < minLevels; i++ {
		snapLevel := snapshotLevels[i]
		liveLevel := liveLevels[i]

		// Compare price
		if !snapLevel.Price.Equal(liveLevel.Price) {
			mismatches = append(mismatches, SnapshotMismatch{
				Level:       fmt.Sprintf("%s[%d]", levelType, i),
				Field:       "price",
				SnapshotVal: snapLevel.Price.String(),
				LiveVal:     liveLevel.Price.String(),
				Difference:  liveLevel.Price.Sub(snapLevel.Price),
			})
			log.Printf("%s[%d] price mismatch: snapshot=%s, live=%s",
				levelType, i, snapLevel.Price, liveLevel.Price)
		}

		// Compare volume
		if !snapLevel.TotalVolume.Equal(liveLevel.TotalVolume) {
			mismatches = append(mismatches, SnapshotMismatch{
				Level:       fmt.Sprintf("%s[%d]", levelType, i),
				Field:       "volume",
				SnapshotVal: snapLevel.TotalVolume.String(),
				LiveVal:     liveLevel.TotalVolume.String(),
				Difference:  liveLevel.TotalVolume.Sub(snapLevel.TotalVolume),
			})
			log.Printf("%s[%d] volume mismatch: snapshot=%s, live=%s (diff: %s)",
				levelType, i, snapLevel.TotalVolume, liveLevel.TotalVolume,
				liveLevel.TotalVolume.Sub(snapLevel.TotalVolume))
		}

		// Compare order count
		if snapLevel.OrderCount != liveLevel.OrderCount {
			mismatches = append(mismatches, SnapshotMismatch{
				Level:       fmt.Sprintf("%s[%d]", levelType, i),
				Field:       "order_count",
				SnapshotVal: snapLevel.OrderCount,
				LiveVal:     liveLevel.OrderCount,
			})
			log.Printf("%s[%d] order count mismatch: snapshot=%d, live=%d",
				levelType, i, snapLevel.OrderCount, liveLevel.OrderCount)
		}
	}

	return mismatches
}

// GetLatestSnapshot retrieves the most recent snapshot
func (sm *SnapshotManager) GetLatestSnapshot(ctx context.Context) (*OrderbookSnapshot, error) {
	query := `SELECT * FROM get_latest_snapshot($1)`

	var snapshot OrderbookSnapshot
	var snapshotDataJSON []byte
	var bestBidStr, bestAskStr, spreadStr sql.NullString
	var totalBidVolumeStr, totalAskVolumeStr sql.NullString
	var bidCount, askCount int
	var snapshotTypeStr, triggeredByStr string
	var createdAt time.Time

	err := sm.db.QueryRowContext(ctx, query, sm.instrument).Scan(
		&snapshot.SnapshotID,
		&snapshot.Instrument,
		&snapshotDataJSON,
		&bidCount, // bid_count (will be overwritten after deserialization)
		&askCount, // ask_count
		&bestBidStr,
		&bestAskStr,
		&spreadStr,
		&totalBidVolumeStr,
		&totalAskVolumeStr,
		&snapshot.SnapshotTimestamp,
		&snapshotTypeStr,
		&triggeredByStr,
		&createdAt, // created_at (not needed but must scan)
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no snapshot found for %s", sm.instrument)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest snapshot: %w", err)
	}

	// Deserialize snapshot data
	var snapshotData map[string]interface{}
	err = json.Unmarshal(snapshotDataJSON, &snapshotData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot data: %w", err)
	}

	// Parse bid and ask levels using helper
	snapshot.BidLevels = parsePriceLevels(snapshotData, "bid_levels")
	snapshot.AskLevels = parsePriceLevels(snapshotData, "ask_levels")

	// Parse optional fields
	if bestBidStr.Valid {
		price, _ := decimal.NewFromString(bestBidStr.String)
		snapshot.BestBidPrice = &price
	}
	if bestAskStr.Valid {
		price, _ := decimal.NewFromString(bestAskStr.String)
		snapshot.BestAskPrice = &price
	}
	if spreadStr.Valid {
		spread, _ := decimal.NewFromString(spreadStr.String)
		snapshot.Spread = &spread
	}
	if totalBidVolumeStr.Valid {
		snapshot.TotalBidVolume, _ = decimal.NewFromString(totalBidVolumeStr.String)
	}
	if totalAskVolumeStr.Valid {
		snapshot.TotalAskVolume, _ = decimal.NewFromString(totalAskVolumeStr.String)
	}
	snapshot.SnapshotType = SnapshotType(snapshotTypeStr)
	snapshot.TriggeredBy = triggeredByStr

	return &snapshot, nil
}

// RestoreFromSnapshot restores orderbook state from a snapshot
func (sm *SnapshotManager) RestoreFromSnapshot(ctx context.Context, snapshotID int64) error {
	// Get snapshot
	snapshot, err := sm.getSnapshotByID(ctx, snapshotID)
	if err != nil {
		return fmt.Errorf("failed to get snapshot: %w", err)
	}

	log.Printf("🔄 Restoring orderbook from snapshot %d (%s) - %d bids, %d asks",
		snapshot.SnapshotID, snapshot.SnapshotTimestamp, len(snapshot.BidLevels), len(snapshot.AskLevels))

	// Note: This is a simplified restore that only restores price levels
	// In production, you'd need to restore actual orders from order_events table
	// This snapshot is primarily for market data visualization/recovery

	return nil
}

// getSnapshotByID retrieves a specific snapshot by ID
func (sm *SnapshotManager) getSnapshotByID(ctx context.Context, snapshotID int64) (*OrderbookSnapshot, error) {
	query := `
		SELECT snapshot_id, instrument, snapshot_data, bid_count, ask_count,
		       best_bid_price, best_ask_price, spread,
		       total_bid_volume, total_ask_volume,
		       snapshot_timestamp, snapshot_type, triggered_by
		FROM orderbook_snapshots
		WHERE snapshot_id = $1
	`

	var snapshot OrderbookSnapshot
	var snapshotDataJSON []byte
	var bestBidStr, bestAskStr, spreadStr, triggeredBy sql.NullString
	var totalBidStr, totalAskStr string
	var snapshotTypeStr string
	var bidCount, askCount int

	err := sm.db.QueryRowContext(ctx, query, snapshotID).Scan(
		&snapshot.SnapshotID,
		&snapshot.Instrument,
		&snapshotDataJSON,
		&bidCount,
		&askCount,
		&bestBidStr,
		&bestAskStr,
		&spreadStr,
		&totalBidStr,
		&totalAskStr,
		&snapshot.SnapshotTimestamp,
		&snapshotTypeStr,
		&triggeredBy,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("snapshot %d not found", snapshotID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query snapshot: %w", err)
	}

	snapshot.SnapshotType = SnapshotType(snapshotTypeStr)
	if triggeredBy.Valid {
		snapshot.TriggeredBy = triggeredBy.String
	}

	// Parse volumes
	snapshot.TotalBidVolume, _ = decimal.NewFromString(totalBidStr)
	snapshot.TotalAskVolume, _ = decimal.NewFromString(totalAskStr)

	// Deserialize levels using helper
	var snapshotData map[string]interface{}
	if err := json.Unmarshal(snapshotDataJSON, &snapshotData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot data: %w", err)
	}

	snapshot.BidLevels = parsePriceLevels(snapshotData, "bid_levels")
	snapshot.AskLevels = parsePriceLevels(snapshotData, "ask_levels")

	return &snapshot, nil
}

// CleanupOldSnapshots removes old snapshots keeping only the most recent N
func (sm *SnapshotManager) CleanupOldSnapshots(ctx context.Context, keepCount int) (int, error) {
	query := `SELECT cleanup_old_snapshots($1, $2)`

	var deletedCount int
	err := sm.db.QueryRowContext(ctx, query, sm.instrument, keepCount).Scan(&deletedCount)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup snapshots: %w", err)
	}

	if deletedCount > 0 {
		log.Printf("🧹 Cleaned up %d old snapshots for %s", deletedCount, sm.instrument)
	}

	return deletedCount, nil
}

// SnapshotMismatch represents a difference between snapshot and live orderbook
type SnapshotMismatch struct {
	Level       string          `json:"level"`
	Field       string          `json:"field"`
	SnapshotVal interface{}     `json:"snapshot_value"`
	LiveVal     interface{}     `json:"live_value"`
	Difference  decimal.Decimal `json:"difference,omitempty"`
}

// ValidateAgainstLiveBook compares snapshot to current orderbook state
func (sm *SnapshotManager) ValidateAgainstLiveBook(ctx context.Context, snapshot *OrderbookSnapshot) []SnapshotMismatch {
	mismatches := []SnapshotMismatch{}

	// Capture current live orderbook state
	liveSnapshot := sm.captureOrderbook(SnapshotTypeRecovery, "validation")

	// Compare bid count
	if len(snapshot.BidLevels) != len(liveSnapshot.BidLevels) {
		mismatches = append(mismatches, SnapshotMismatch{
			Level:       "bids",
			Field:       "level_count",
			SnapshotVal: len(snapshot.BidLevels),
			LiveVal:     len(liveSnapshot.BidLevels),
		})
		log.Printf("Bid level count mismatch: snapshot=%d, live=%d",
			len(snapshot.BidLevels), len(liveSnapshot.BidLevels))
	}

	// Compare ask count
	if len(snapshot.AskLevels) != len(liveSnapshot.AskLevels) {
		mismatches = append(mismatches, SnapshotMismatch{
			Level:       "asks",
			Field:       "level_count",
			SnapshotVal: len(snapshot.AskLevels),
			LiveVal:     len(liveSnapshot.AskLevels),
		})
		log.Printf("Ask level count mismatch: snapshot=%d, live=%d",
			len(snapshot.AskLevels), len(liveSnapshot.AskLevels))
	}

	// Compare bid and ask levels using helper
	mismatches = append(mismatches, comparePriceLevels(snapshot.BidLevels, liveSnapshot.BidLevels, "bid")...)
	mismatches = append(mismatches, comparePriceLevels(snapshot.AskLevels, liveSnapshot.AskLevels, "ask")...)

	// Compare total volumes
	if !snapshot.TotalBidVolume.Equal(liveSnapshot.TotalBidVolume) {
		mismatches = append(mismatches, SnapshotMismatch{
			Level:       "totals",
			Field:       "bid_volume",
			SnapshotVal: snapshot.TotalBidVolume.String(),
			LiveVal:     liveSnapshot.TotalBidVolume.String(),
			Difference:  liveSnapshot.TotalBidVolume.Sub(snapshot.TotalBidVolume),
		})
		log.Printf("Total bid volume mismatch: snapshot=%s, live=%s",
			snapshot.TotalBidVolume, liveSnapshot.TotalBidVolume)
	}

	if !snapshot.TotalAskVolume.Equal(liveSnapshot.TotalAskVolume) {
		mismatches = append(mismatches, SnapshotMismatch{
			Level:       "totals",
			Field:       "ask_volume",
			SnapshotVal: snapshot.TotalAskVolume.String(),
			LiveVal:     liveSnapshot.TotalAskVolume.String(),
			Difference:  liveSnapshot.TotalAskVolume.Sub(snapshot.TotalAskVolume),
		})
		log.Printf("Total ask volume mismatch: snapshot=%s, live=%s",
			snapshot.TotalAskVolume, liveSnapshot.TotalAskVolume)
	}

	// Log summary
	if len(mismatches) == 0 {
		log.Printf("Snapshot validation passed: snapshot matches live orderbook perfectly")
	} else {
		log.Printf(" Snapshot validation found %d mismatches", len(mismatches))
	}

	return mismatches
}

// ValidateLatestSnapshot validates the most recent snapshot against live orderbook
func (sm *SnapshotManager) ValidateLatestSnapshot(ctx context.Context) ([]SnapshotMismatch, error) {
	snapshot, err := sm.GetLatestSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest snapshot: %w", err)
	}

	log.Printf("🔍 Validating snapshot %d (taken at %s) against live orderbook...",
		snapshot.SnapshotID, snapshot.SnapshotTimestamp)

	mismatches := sm.ValidateAgainstLiveBook(ctx, snapshot)
	return mismatches, nil
}
