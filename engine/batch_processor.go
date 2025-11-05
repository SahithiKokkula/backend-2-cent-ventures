package engine

import (
	"context"
	"sync"
	"time"

	"github.com/yourusername/trading-engine/models"
)

type BatchProcessor struct {
	eventBatch     []*Event
	eventBatchSize int
	eventMu        sync.Mutex
	flushInterval  time.Duration

	tradeBatch     []*Trade
	tradeBatchSize int
	tradeMu        sync.Mutex

	orderBatch     []*models.Order
	orderBatchSize int
	orderMu        sync.Mutex

	eventHandler func([]*Event)
	tradeHandler func([]*Trade)
	orderHandler func([]*models.Order)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	eventBatchesProcessed uint64
	tradeBatchesProcessed uint64
	orderBatchesProcessed uint64
	eventCount            uint64
	tradeCount            uint64
	orderCount            uint64
	statsMu               sync.RWMutex
}

type BatchConfig struct {
	EventBatchSize int
	TradeBatchSize int
	OrderBatchSize int
	FlushInterval  time.Duration
}

func DefaultBatchConfig() *BatchConfig {
	return &BatchConfig{
		EventBatchSize: 100,
		TradeBatchSize: 50,
		OrderBatchSize: 100,
		FlushInterval:  10 * time.Millisecond,
	}
}

func NewBatchProcessor(config *BatchConfig) *BatchProcessor {
	if config == nil {
		config = DefaultBatchConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	bp := &BatchProcessor{
		eventBatch:     make([]*Event, 0, config.EventBatchSize),
		eventBatchSize: config.EventBatchSize,
		tradeBatch:     make([]*Trade, 0, config.TradeBatchSize),
		tradeBatchSize: config.TradeBatchSize,
		orderBatch:     make([]*models.Order, 0, config.OrderBatchSize),
		orderBatchSize: config.OrderBatchSize,
		flushInterval:  config.FlushInterval,
		ctx:            ctx,
		cancel:         cancel,
	}

	return bp
}

func (bp *BatchProcessor) Start() {
	bp.wg.Add(1)
	go bp.periodicFlush()
}

func (bp *BatchProcessor) Stop() {
	bp.cancel()
	bp.wg.Wait()

	// Flush any remaining items
	bp.FlushEvents()
	bp.FlushTrades()
	bp.FlushOrders()
}

// SetEventHandler sets the handler for event batches
func (bp *BatchProcessor) SetEventHandler(handler func([]*Event)) {
	bp.eventHandler = handler
}

// SetTradeHandler sets the handler for trade batches
func (bp *BatchProcessor) SetTradeHandler(handler func([]*Trade)) {
	bp.tradeHandler = handler
}

// SetOrderHandler sets the handler for order batches
func (bp *BatchProcessor) SetOrderHandler(handler func([]*models.Order)) {
	bp.orderHandler = handler
}

// AddEvent adds an event to the batch
func (bp *BatchProcessor) AddEvent(event *Event) {
	bp.eventMu.Lock()
	defer bp.eventMu.Unlock()

	bp.eventBatch = append(bp.eventBatch, event)
	bp.eventCount++

	// Flush if batch is full
	if len(bp.eventBatch) >= bp.eventBatchSize {
		bp.flushEventsLocked()
	}
}

// AddTrade adds a trade to the batch
func (bp *BatchProcessor) AddTrade(trade *Trade) {
	bp.tradeMu.Lock()
	defer bp.tradeMu.Unlock()

	bp.tradeBatch = append(bp.tradeBatch, trade)
	bp.tradeCount++

	// Flush if batch is full
	if len(bp.tradeBatch) >= bp.tradeBatchSize {
		bp.flushTradesLocked()
	}
}

// AddOrder adds an order update to the batch
func (bp *BatchProcessor) AddOrder(order *models.Order) {
	bp.orderMu.Lock()
	defer bp.orderMu.Unlock()

	bp.orderBatch = append(bp.orderBatch, order)
	bp.orderCount++

	// Flush if batch is full
	if len(bp.orderBatch) >= bp.orderBatchSize {
		bp.flushOrdersLocked()
	}
}

// FlushEvents flushes all pending events
func (bp *BatchProcessor) FlushEvents() {
	bp.eventMu.Lock()
	defer bp.eventMu.Unlock()
	bp.flushEventsLocked()
}

// flushEventsLocked flushes events (caller must hold lock)
func (bp *BatchProcessor) flushEventsLocked() {
	if len(bp.eventBatch) == 0 {
		return
	}

	if bp.eventHandler != nil {
		// Make a copy to avoid holding lock during handler execution
		batch := make([]*Event, len(bp.eventBatch))
		copy(batch, bp.eventBatch)

		// Release lock before calling handler
		bp.eventMu.Unlock()
		bp.eventHandler(batch)
		bp.eventMu.Lock()
	}

	// Clear batch but keep capacity
	bp.eventBatch = bp.eventBatch[:0]

	bp.statsMu.Lock()
	bp.eventBatchesProcessed++
	bp.statsMu.Unlock()
}

// FlushTrades flushes all pending trades
func (bp *BatchProcessor) FlushTrades() {
	bp.tradeMu.Lock()
	defer bp.tradeMu.Unlock()
	bp.flushTradesLocked()
}

// flushTradesLocked flushes trades (caller must hold lock)
func (bp *BatchProcessor) flushTradesLocked() {
	if len(bp.tradeBatch) == 0 {
		return
	}

	if bp.tradeHandler != nil {
		// Make a copy to avoid holding lock during handler execution
		batch := make([]*Trade, len(bp.tradeBatch))
		copy(batch, bp.tradeBatch)

		// Release lock before calling handler
		bp.tradeMu.Unlock()
		bp.tradeHandler(batch)
		bp.tradeMu.Lock()
	}

	// Clear batch but keep capacity
	bp.tradeBatch = bp.tradeBatch[:0]

	bp.statsMu.Lock()
	bp.tradeBatchesProcessed++
	bp.statsMu.Unlock()
}

// FlushOrders flushes all pending order updates
func (bp *BatchProcessor) FlushOrders() {
	bp.orderMu.Lock()
	defer bp.orderMu.Unlock()
	bp.flushOrdersLocked()
}

// flushOrdersLocked flushes orders (caller must hold lock)
func (bp *BatchProcessor) flushOrdersLocked() {
	if len(bp.orderBatch) == 0 {
		return
	}

	if bp.orderHandler != nil {
		// Make a copy to avoid holding lock during handler execution
		batch := make([]*models.Order, len(bp.orderBatch))
		copy(batch, bp.orderBatch)

		// Release lock before calling handler
		bp.orderMu.Unlock()
		bp.orderHandler(batch)
		bp.orderMu.Lock()
	}

	// Clear batch but keep capacity
	bp.orderBatch = bp.orderBatch[:0]

	bp.statsMu.Lock()
	bp.orderBatchesProcessed++
	bp.statsMu.Unlock()
}

// periodicFlush flushes batches periodically
func (bp *BatchProcessor) periodicFlush() {
	defer bp.wg.Done()

	ticker := time.NewTicker(bp.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bp.ctx.Done():
			return
		case <-ticker.C:
			bp.FlushEvents()
			bp.FlushTrades()
			bp.FlushOrders()
		}
	}
}

// BatchStats holds batch processor statistics
type BatchStats struct {
	EventBatchesProcessed uint64
	TradeBatchesProcessed uint64
	OrderBatchesProcessed uint64
	EventCount            uint64
	TradeCount            uint64
	OrderCount            uint64
	AvgEventsPerBatch     float64
	AvgTradesPerBatch     float64
	AvgOrdersPerBatch     float64
}

// GetStats returns current batch processor statistics
func (bp *BatchProcessor) GetStats() BatchStats {
	bp.statsMu.RLock()
	defer bp.statsMu.RUnlock()

	stats := BatchStats{
		EventBatchesProcessed: bp.eventBatchesProcessed,
		TradeBatchesProcessed: bp.tradeBatchesProcessed,
		OrderBatchesProcessed: bp.orderBatchesProcessed,
		EventCount:            bp.eventCount,
		TradeCount:            bp.tradeCount,
		OrderCount:            bp.orderCount,
	}

	// Calculate averages
	if stats.EventBatchesProcessed > 0 {
		stats.AvgEventsPerBatch = float64(stats.EventCount) / float64(stats.EventBatchesProcessed)
	}
	if stats.TradeBatchesProcessed > 0 {
		stats.AvgTradesPerBatch = float64(stats.TradeCount) / float64(stats.TradeBatchesProcessed)
	}
	if stats.OrderBatchesProcessed > 0 {
		stats.AvgOrdersPerBatch = float64(stats.OrderCount) / float64(stats.OrderBatchesProcessed)
	}

	return stats
}

// ResetStats resets batch processor statistics
func (bp *BatchProcessor) ResetStats() {
	bp.statsMu.Lock()
	defer bp.statsMu.Unlock()

	bp.eventBatchesProcessed = 0
	bp.tradeBatchesProcessed = 0
	bp.orderBatchesProcessed = 0
	bp.eventCount = 0
	bp.tradeCount = 0
	bp.orderCount = 0
}
