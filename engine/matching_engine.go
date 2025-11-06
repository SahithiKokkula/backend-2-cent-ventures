package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/metrics"
	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Trade represents a matched trade between two orders
type Trade struct {
	TradeID     uuid.UUID
	BuyOrderID  uuid.UUID
	SellOrderID uuid.UUID
	Instrument  string
	Price       decimal.Decimal
	Quantity    decimal.Decimal
	Timestamp   time.Time
	// Debug information
	IsBuyerPartialFill  bool            // True if buy order was partially filled
	IsSellerPartialFill bool            // True if sell order was partially filled
	BuyerFilledQty      decimal.Decimal // Total filled quantity for buyer after this trade
	SellerFilledQty     decimal.Decimal // Total filled quantity for seller after this trade
	BuyerRemainingQty   decimal.Decimal // Remaining quantity for buyer after this trade
	SellerRemainingQty  decimal.Decimal // Remaining quantity for seller after this trade
}

// NewTrade creates a new trade
func NewTrade(buyOrderID, sellOrderID uuid.UUID, instrument string, price, quantity decimal.Decimal) *Trade {
	return &Trade{
		TradeID:     uuid.New(),
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Instrument:  instrument,
		Price:       price,
		Quantity:    quantity,
		Timestamp:   time.Now(),
	}
}

// TradeDebugInfo holds detailed debug information about a trade execution
type TradeDebugInfo struct {
	TradeID           uuid.UUID
	ExecutionTime     time.Time
	ExecutionPrice    decimal.Decimal
	ExecutionQuantity decimal.Decimal

	// Buyer information
	BuyOrderID         uuid.UUID
	BuyerSide          models.OrderSide
	BuyerLimitPrice    decimal.Decimal
	BuyerTotalQty      decimal.Decimal
	BuyerFilledBefore  decimal.Decimal
	BuyerFilledAfter   decimal.Decimal
	BuyerRemainingQty  decimal.Decimal
	BuyerStatusBefore  models.OrderStatus
	BuyerStatusAfter   models.OrderStatus
	IsBuyerPartialFill bool

	// Seller information
	SellOrderID         uuid.UUID
	SellerSide          models.OrderSide
	SellerLimitPrice    decimal.Decimal
	SellerTotalQty      decimal.Decimal
	SellerFilledBefore  decimal.Decimal
	SellerFilledAfter   decimal.Decimal
	SellerRemainingQty  decimal.Decimal
	SellerStatusBefore  models.OrderStatus
	SellerStatusAfter   models.OrderStatus
	IsSellerPartialFill bool

	// Match information
	MatchLevel   int // Which price level this match occurred at (for multi-level matches)
	FIFOPosition int // Position in FIFO queue (1 = first, 2 = second, etc.)
}

// String returns a formatted debug string for the trade
func (tdi *TradeDebugInfo) String() string {
	return fmt.Sprintf(
		"[TRADE DEBUG] TradeID=%s, Time=%s\n"+
			"  Execution: Price=%s, Qty=%s\n"+
			"  Buyer: OrderID=%s, LimitPrice=%s, TotalQty=%s\n"+
			"    Before: Filled=%s, Status=%v\n"+
			"    After:  Filled=%s, Remaining=%s, Status=%v, PartialFill=%v\n"+
			"  Seller: OrderID=%s, LimitPrice=%s, TotalQty=%s\n"+
			"    Before: Filled=%s, Status=%v\n"+
			"    After:  Filled=%s, Remaining=%s, Status=%v, PartialFill=%v\n"+
			"  Match: Level=%d, FIFOPosition=%d",
		tdi.TradeID, tdi.ExecutionTime.Format("15:04:05.000000"),
		tdi.ExecutionPrice, tdi.ExecutionQuantity,
		tdi.BuyOrderID, tdi.BuyerLimitPrice, tdi.BuyerTotalQty,
		tdi.BuyerFilledBefore, tdi.BuyerStatusBefore,
		tdi.BuyerFilledAfter, tdi.BuyerRemainingQty, tdi.BuyerStatusAfter, tdi.IsBuyerPartialFill,
		tdi.SellOrderID, tdi.SellerLimitPrice, tdi.SellerTotalQty,
		tdi.SellerFilledBefore, tdi.SellerStatusBefore,
		tdi.SellerFilledAfter, tdi.SellerRemainingQty, tdi.SellerStatusAfter, tdi.IsSellerPartialFill,
		tdi.MatchLevel, tdi.FIFOPosition,
	)
}

// OrderCommand represents a command to the matching engine
type OrderCommand struct {
	Type     string                // "NEW", "CANCEL"
	Order    *models.Order         // For NEW commands
	OrderID  uuid.UUID             // For CANCEL commands
	Response chan *CommandResponse // Channel to send response back
}

// CommandResponse represents the response from processing a command
type CommandResponse struct {
	Trades []*Trade
	Error  error
	Order  *models.Order // The processed order with updated status
}

// MatchingEngine handles order matching with a single-threaded worker
type MatchingEngine struct {
	orderBook    *OrderBook
	commandChan  chan *OrderCommand
	stopChan     chan struct{}
	wg           sync.WaitGroup
	isRunning    bool
	mu           sync.RWMutex
	tradeHandler func(*Trade)

	eventBus *EventBus

	debugMode      bool
	debugLogger    *log.Logger
	tradeDebugInfo []*TradeDebugInfo

	commandsProcessed uint64
	channelBlocks     uint64

	wsTradeHandler       func(trade *Trade)
	wsOrderbookHandler   func(instrument, side string, price, size decimal.Decimal)
	wsOrderUpdateHandler func(order *models.Order)
}

func NewMatchingEngine(instrument string) *MatchingEngine {
	return &MatchingEngine{
		orderBook:      NewOrderBook(instrument),
		commandChan:    make(chan *OrderCommand, 1000),
		stopChan:       make(chan struct{}),
		isRunning:      false,
		eventBus:       NewEventBus(),
		debugMode:      false,
		debugLogger:    log.New(log.Writer(), "[MatchingEngine] ", log.LstdFlags|log.Lmicroseconds),
		tradeDebugInfo: make([]*TradeDebugInfo, 0),
	}
}

func (me *MatchingEngine) EnableDebugMode(enable bool) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.debugMode = enable
}

func (me *MatchingEngine) IsDebugMode() bool {
	me.mu.RLock()
	defer me.mu.RUnlock()
	return me.debugMode
}

func (me *MatchingEngine) SetDebugLogger(logger *log.Logger) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.debugLogger = logger
}

func (me *MatchingEngine) GetTradeDebugInfo() []*TradeDebugInfo {
	me.mu.RLock()
	defer me.mu.RUnlock()
	return me.tradeDebugInfo
}

func (me *MatchingEngine) ClearTradeDebugInfo() {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.tradeDebugInfo = make([]*TradeDebugInfo, 0)
}

func (me *MatchingEngine) debugLog(format string, args ...interface{}) {
	me.mu.RLock()
	debug := me.debugMode
	logger := me.debugLogger
	me.mu.RUnlock()

	if debug && logger != nil {
		logger.Printf(format, args...)
	}
}

func (me *MatchingEngine) SetTradeHandler(handler func(*Trade)) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.tradeHandler = handler
}

func (me *MatchingEngine) SetWebSocketHandlers(
	tradeHandler func(trade *Trade),
	orderbookHandler func(instrument, side string, price, size decimal.Decimal),
	orderUpdateHandler func(order *models.Order),
) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.wsTradeHandler = tradeHandler
	me.wsOrderbookHandler = orderbookHandler
	me.wsOrderUpdateHandler = orderUpdateHandler
}

func (me *MatchingEngine) GetEventBus() *EventBus {
	return me.eventBus
}

func (me *MatchingEngine) SubscribeToEvents(eventType EventType, listener EventListener) {
	me.eventBus.Subscribe(eventType, listener)
}

func (me *MatchingEngine) publishOrderbookChange(side string, action string, price, newSize, oldSize decimal.Decimal) {
	event := Event{
		Type:      EventTypeOrderbookChange,
		Timestamp: time.Now(),
		Data: OrderbookChangeEvent{
			Instrument: me.orderBook.Instrument,
			Side:       side,
			Action:     action,
			Price:      price,
			NewSize:    newSize,
			OldSize:    oldSize,
			Timestamp:  time.Now(),
		},
	}
	me.eventBus.Publish(event)
}

// Start begins the single-threaded matching worker
// WHY THIS AVOIDS RACE CONDITIONS:
// 1. Single goroutine: Only ONE goroutine ever accesses/mutates the orderBook
// 2. Sequential processing: Commands are processed one at a time in order
// 3. No shared memory: The orderBook is not accessed from multiple threads
// 4. Channel synchronization: Go channels provide built-in synchronization
// 5. Message passing: We use message passing (commands) instead of shared memory
//
// This is the "share memory by communicating" principle in Go, rather than
// "communicating by sharing memory". The orderBook state is owned by a single
// goroutine, eliminating all race conditions without needing locks on the orderBook.
func (me *MatchingEngine) Start(ctx context.Context) error {
	me.mu.Lock()
	if me.isRunning {
		me.mu.Unlock()
		return fmt.Errorf("matching engine is already running")
	}
	me.isRunning = true
	me.mu.Unlock()

	me.wg.Add(1)
	go me.matchingWorker(ctx)

	return nil
}

// Stop gracefully shuts down the matching engine
func (me *MatchingEngine) Stop() error {
	me.mu.Lock()
	if !me.isRunning {
		me.mu.Unlock()
		return fmt.Errorf("matching engine is not running")
	}
	me.mu.Unlock()

	// Signal worker to stop
	close(me.stopChan)

	// Wait for worker to finish processing current command
	me.wg.Wait()

	me.mu.Lock()
	me.isRunning = false
	me.mu.Unlock()

	return nil
}

// IsRunning returns whether the matching engine is currently running
func (me *MatchingEngine) IsRunning() bool {
	me.mu.RLock()
	defer me.mu.RUnlock()
	return me.isRunning
}

// matchingWorker is the single-threaded worker that processes all commands
// This is the ONLY goroutine that ever touches the orderBook, ensuring thread safety
func (me *MatchingEngine) matchingWorker(ctx context.Context) {
	defer me.wg.Done()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, drain remaining commands and exit
			me.drainCommands()
			return

		case <-me.stopChan:
			// Stop signal received, drain remaining commands and exit
			me.drainCommands()
			return

		case cmd := <-me.commandChan:
			// Process command sequentially
			me.processCommand(cmd)
		}
	}
}

// drainCommands processes any remaining commands in the channel before stopping
func (me *MatchingEngine) drainCommands() {
	for {
		select {
		case cmd := <-me.commandChan:
			me.processCommand(cmd)
		default:
			return
		}
	}
}

// processCommand handles a single command (only called by matchingWorker)
func (me *MatchingEngine) processCommand(cmd *OrderCommand) {
	me.mu.Lock()
	me.commandsProcessed++
	commandNum := me.commandsProcessed
	me.mu.Unlock()

	me.debugLog("=== Processing Command #%d: Type=%s ===", commandNum, cmd.Type)

	var response *CommandResponse

	switch cmd.Type {
	case "NEW":
		me.debugLog("Command #%d: NEW order - ID=%s, Side=%v, Type=%v, Price=%s, Qty=%s",
			commandNum, cmd.Order.ID, cmd.Order.Side, cmd.Order.Type, cmd.Order.Price, cmd.Order.Quantity)

		// Snapshot state before matching
		me.debugLog("Command #%d: OrderBook before matching - Size=%d, BidLevels=%d, AskLevels=%d",
			commandNum, me.orderBook.Size(), me.orderBook.Bids.Len(), me.orderBook.Asks.Len())

		if bestBid := me.orderBook.GetBestBid(); bestBid != nil {
			me.debugLog("Command #%d: Best Bid: Price=%s, Volume=%s", commandNum, bestBid.Price, bestBid.Volume)
		}
		if bestAsk := me.orderBook.GetBestAsk(); bestAsk != nil {
			me.debugLog("Command #%d: Best Ask: Price=%s, Volume=%s", commandNum, bestAsk.Price, bestAsk.Volume)
		}

		trades := me.matchOrder(cmd.Order)

		me.debugLog("Command #%d: Matching complete - Trades=%d, OrderStatus=%v, FilledQty=%s",
			commandNum, len(trades), cmd.Order.Status, cmd.Order.FilledQuantity)

		// Snapshot state after matching
		me.debugLog("Command #%d: OrderBook after matching - Size=%d, BidLevels=%d, AskLevels=%d",
			commandNum, me.orderBook.Size(), me.orderBook.Bids.Len(), me.orderBook.Asks.Len())

		response = &CommandResponse{
			Trades: trades,
			Order:  cmd.Order,
		}

	case "CANCEL":
		me.debugLog("Command #%d: CANCEL order - ID=%s", commandNum, cmd.OrderID)

		cancelledOrder := me.orderBook.CancelOrder(cmd.OrderID)
		if cancelledOrder == nil {
			me.debugLog("Command #%d: Order not found: %s", commandNum, cmd.OrderID)
			response = &CommandResponse{
				Error: fmt.Errorf("order not found: %s", cmd.OrderID),
			}
		} else {
			me.debugLog("Command #%d: Order cancelled successfully - ID=%s", commandNum, cmd.OrderID)

			// Publish orderbook change event for cancelled order
			side := "buy"
			if cancelledOrder.Side == models.OrderSideSell {
				side = "sell"
			}
			me.publishOrderbookChange(side, "remove", cancelledOrder.Price, decimal.Zero, cancelledOrder.RemainingQuantity())

			response = &CommandResponse{
				Order: cancelledOrder,
			}
		}

	default:
		me.debugLog("Command #%d: UNKNOWN command type: %s", commandNum, cmd.Type)
		response = &CommandResponse{
			Error: fmt.Errorf("unknown command type: %s", cmd.Type),
		}
	}

	me.debugLog("=== Command #%d Complete ===", commandNum)

	// Send response back to caller
	if cmd.Response != nil {
		cmd.Response <- response
		close(cmd.Response)
	}
}

// SubmitOrder submits a new order to the matching engine
// This is thread-safe because it only sends a message to the worker
func (me *MatchingEngine) SubmitOrder(order *models.Order) (*CommandResponse, error) {
	me.mu.RLock()
	if !me.isRunning {
		me.mu.RUnlock()
		return nil, fmt.Errorf("matching engine is not running")
	}
	me.mu.RUnlock()

	responseChan := make(chan *CommandResponse, 1)
	cmd := &OrderCommand{
		Type:     "NEW",
		Order:    order,
		Response: responseChan,
	}

	// DEBUG: Check if channel might block
	channelLen := len(me.commandChan)
	channelCap := cap(me.commandChan)
	me.debugLog("SubmitOrder: channelLen=%d, channelCap=%d, utilization=%.1f%%",
		channelLen, channelCap, float64(channelLen)/float64(channelCap)*100)

	if channelLen >= channelCap-10 {
		me.debugLog("WARNING: Command channel nearly full! %d/%d commands buffered", channelLen, channelCap)
		me.mu.Lock()
		me.channelBlocks++
		me.mu.Unlock()
	}

	select {
	case me.commandChan <- cmd:
		me.debugLog("SubmitOrder: Command posted for order %s (ClientID=%s, Side=%v, Price=%s, Qty=%s)",
			order.ID, order.ClientID, order.Side, order.Price, order.Quantity)

		response := <-responseChan
		me.debugLog("SubmitOrder: Response received for order %s, Trades=%d", order.ID, len(response.Trades))
		return response, response.Error
	default:
		me.mu.Lock()
		me.channelBlocks++
		me.mu.Unlock()
		me.debugLog("ERROR: Command channel is FULL! Cannot submit order %s", order.ID)
		return nil, fmt.Errorf("command channel is full")
	}
}

func (me *MatchingEngine) CancelOrder(orderID uuid.UUID) (*CommandResponse, error) {
	me.mu.RLock()
	if !me.isRunning {
		me.mu.RUnlock()
		return nil, fmt.Errorf("matching engine is not running")
	}
	me.mu.RUnlock()

	responseChan := make(chan *CommandResponse, 1)
	cmd := &OrderCommand{
		Type:     "CANCEL",
		OrderID:  orderID,
		Response: responseChan,
	}

	channelLen := len(me.commandChan)
	channelCap := cap(me.commandChan)
	me.debugLog("CancelOrder: channelLen=%d, channelCap=%d", channelLen, channelCap)

	select {
	case me.commandChan <- cmd:
		me.debugLog("CancelOrder: Command posted for order %s", orderID)

		response := <-responseChan
		me.debugLog("CancelOrder: Response received for order %s, Error=%v", orderID, response.Error)
		return response, response.Error
	default:
		me.mu.Lock()
		me.channelBlocks++
		me.mu.Unlock()
		me.debugLog("ERROR: Command channel is FULL! Cannot cancel order %s", orderID)
		return nil, fmt.Errorf("command channel is full")
	}
}

func (me *MatchingEngine) matchOrder(newOrder *models.Order) []*Trade {
	trades := make([]*Trade, 0)

	// Validate order
	if !newOrder.IsValid() {
		newOrder.Status = models.OrderStatusRejected
		return trades
	}

	if newOrder.Type == models.OrderTypeMarket {
		trades = me.matchMarketOrder(newOrder)
	} else {
		trades = me.matchLimitOrder(newOrder)
	}

	if newOrder.Type == models.OrderTypeLimit && newOrder.CanBeFilled() && !newOrder.IsFilled() {
		me.orderBook.AddOrder(newOrder)

		me.updateOrderbookMetrics(newOrder.Instrument)

		side := "buy"
		if newOrder.Side == models.OrderSideSell {
			side = "sell"
		}
		me.publishOrderbookChange(side, "add", newOrder.Price, newOrder.RemainingQuantity(), decimal.Zero)

		me.mu.RLock()
		wsOrderbookHandler := me.wsOrderbookHandler
		me.mu.RUnlock()

		if wsOrderbookHandler != nil {
			wsOrderbookHandler(newOrder.Instrument, side, newOrder.Price, newOrder.RemainingQuantity())
		}
	}

	me.mu.RLock()
	handler := me.tradeHandler
	wsTradeHandler := me.wsTradeHandler
	wsOrderUpdateHandler := me.wsOrderUpdateHandler
	me.mu.RUnlock()

	if handler != nil {
		for _, trade := range trades {
			handler(trade)
		}
	}

	// Publish NewTrade events for each trade (event-driven architecture)
	for _, trade := range trades {
		tradeEvent := NewTradeEvent{
			TradeID:     trade.TradeID,
			Instrument:  trade.Instrument,
			BuyOrderID:  trade.BuyOrderID,
			SellOrderID: trade.SellOrderID,
			Price:       trade.Price,
			Quantity:    trade.Quantity,
			Timestamp:   trade.Timestamp,
		}

		event := Event{
			Type:      EventTypeNewTrade,
			Timestamp: time.Now(),
			Data:      tradeEvent,
		}

		me.eventBus.Publish(event)
	}

	// Broadcast trades via WebSocket (legacy handlers - will be replaced by event listeners)
	if wsTradeHandler != nil {
		for _, trade := range trades {
			wsTradeHandler(trade)
		}
	}

	// Broadcast order updates via WebSocket (aggressor order and affected orders)
	if wsOrderUpdateHandler != nil {
		wsOrderUpdateHandler(newOrder)
	}

	return trades
}

// matchMarketOrder matches a market order (executes at best available prices)
func (me *MatchingEngine) matchMarketOrder(order *models.Order) []*Trade {
	trades := make([]*Trade, 0)

	// Market orders execute immediately at best available price or get rejected
	for order.RemainingQuantity().GreaterThan(decimal.Zero) {
		// Get the best opposing price level
		var bestLevel *PriceLevel
		if order.Side == models.OrderSideBuy {
			bestLevel = me.orderBook.GetBestAsk()
		} else {
			bestLevel = me.orderBook.GetBestBid()
		}

		if bestLevel == nil {
			// No more opposing orders
			// If we already filled some quantity, mark as partially filled
			// Otherwise, reject the order
			if order.FilledQuantity.GreaterThan(decimal.Zero) {
				order.Status = models.OrderStatusPartiallyFilled
			} else {
				order.Status = models.OrderStatusRejected
			}
			break
		}

		// Match against orders at this price level
		tradesAtLevel := me.matchAgainstPriceLevel(order, bestLevel)
		trades = append(trades, tradesAtLevel...)

		// If price level is empty, it will be removed by matchAgainstPriceLevel
		if order.IsFilled() {
			break
		}
	}

	return trades
}

// matchLimitOrder matches a limit order (only executes at specified price or better)
func (me *MatchingEngine) matchLimitOrder(order *models.Order) []*Trade {
	trades := make([]*Trade, 0)

	for order.RemainingQuantity().GreaterThan(decimal.Zero) {
		// Get the best opposing price level
		var bestLevel *PriceLevel
		if order.Side == models.OrderSideBuy {
			bestLevel = me.orderBook.GetBestAsk()
		} else {
			bestLevel = me.orderBook.GetBestBid()
		}

		if bestLevel == nil {
			// No opposing orders
			break
		}

		// Check if price crosses (can match)
		canMatch := false
		if order.Side == models.OrderSideBuy {
			// Buy order can match if bid price >= ask price
			canMatch = order.Price.GreaterThanOrEqual(bestLevel.Price)
		} else {
			// Sell order can match if ask price <= bid price
			canMatch = order.Price.LessThanOrEqual(bestLevel.Price)
		}

		if !canMatch {
			// Price doesn't cross, stop matching
			break
		}

		// Match against orders at this price level
		tradesAtLevel := me.matchAgainstPriceLevel(order, bestLevel)
		trades = append(trades, tradesAtLevel...)

		if order.IsFilled() {
			break
		}
	}

	return trades
}

// matchAgainstPriceLevel matches an order against all orders in a price level (FIFO)
func (me *MatchingEngine) matchAgainstPriceLevel(aggressorOrder *models.Order, priceLevel *PriceLevel) []*Trade {
	trades := make([]*Trade, 0)

	me.debugLog("  Matching against price level: Price=%s, Orders=%d, Volume=%s",
		priceLevel.Price, priceLevel.Orders.Len(), priceLevel.Volume)

	// Iterate through orders in FIFO order
	element := priceLevel.Orders.Front()
	orderIndex := 0
	matchLevel := len(trades) + 1 // Track which level we're matching at

	for element != nil && aggressorOrder.RemainingQuantity().GreaterThan(decimal.Zero) {
		nextElement := element.Next() // Save next before we potentially remove current
		restingOrder := element.Value.(*models.Order)
		orderIndex++

		me.debugLog("    [FIFO #%d] Resting order: ID=%s, Price=%s, Remaining=%s",
			orderIndex, restingOrder.ID, restingOrder.Price, restingOrder.RemainingQuantity())

		// Capture state BEFORE the fill for debugging
		var buyOrder, sellOrder *models.Order
		if aggressorOrder.Side == models.OrderSideBuy {
			buyOrder = aggressorOrder
			sellOrder = restingOrder
		} else {
			buyOrder = restingOrder
			sellOrder = aggressorOrder
		}

		buyerStatusBefore := buyOrder.Status
		sellerStatusBefore := sellOrder.Status
		buyerFilledBefore := buyOrder.FilledQuantity
		sellerFilledBefore := sellOrder.FilledQuantity

		// Calculate match quantity (minimum of remaining quantities)
		matchQty := decimal.Min(aggressorOrder.RemainingQuantity(), restingOrder.RemainingQuantity())

		tradePrice := restingOrder.Price

		me.debugLog("    [FIFO #%d] MATCH: Qty=%s at Price=%s", orderIndex, matchQty, tradePrice)
		me.debugLog("    [FIFO #%d] SUB-FILL EXECUTION: Price=%s, Qty=%s, Aggressor=%s, Resting=%s",
			orderIndex, tradePrice, matchQty, aggressorOrder.ID, restingOrder.ID)

		var trade *Trade
		if aggressorOrder.Side == models.OrderSideBuy {
			trade = NewTrade(aggressorOrder.ID, restingOrder.ID, aggressorOrder.Instrument, tradePrice, matchQty)
		} else {
			trade = NewTrade(restingOrder.ID, aggressorOrder.ID, aggressorOrder.Instrument, tradePrice, matchQty)
		}

		aggressorOrder.Fill(matchQty)
		restingOrder.Fill(matchQty)

		buyerStatusAfter := buyOrder.Status
		sellerStatusAfter := sellOrder.Status
		buyerFilledAfter := buyOrder.FilledQuantity
		sellerFilledAfter := sellOrder.FilledQuantity

		trade.IsBuyerPartialFill = buyOrder.Status == models.OrderStatusPartiallyFilled
		trade.IsSellerPartialFill = sellOrder.Status == models.OrderStatusPartiallyFilled
		trade.BuyerFilledQty = buyOrder.FilledQuantity
		trade.SellerFilledQty = sellOrder.FilledQuantity
		trade.BuyerRemainingQty = buyOrder.RemainingQuantity()
		trade.SellerRemainingQty = sellOrder.RemainingQuantity()

		trades = append(trades, trade)

		me.debugLog("    [FIFO #%d] Trade created: TradeID=%s, BuyOrder=%s, SellOrder=%s",
			orderIndex, trade.TradeID, trade.BuyOrderID, trade.SellOrderID)

		if trade.IsBuyerPartialFill || trade.IsSellerPartialFill {
			me.debugLog("    [FIFO #%d] PARTIAL FILL DETECTED:", orderIndex)
			if trade.IsBuyerPartialFill {
				me.debugLog("      Buyer (Order %s): Filled=%s/%s, Remaining=%s, Status=%v",
					buyOrder.ID, trade.BuyerFilledQty, buyOrder.Quantity, trade.BuyerRemainingQty, buyOrder.Status)
			}
			if trade.IsSellerPartialFill {
				me.debugLog("      Seller (Order %s): Filled=%s/%s, Remaining=%s, Status=%v",
					sellOrder.ID, trade.SellerFilledQty, sellOrder.Quantity, trade.SellerRemainingQty, sellOrder.Status)
			}
		}

		me.debugLog("    [FIFO #%d] After fill - Aggressor: Filled=%s, Remaining=%s, Status=%v",
			orderIndex, aggressorOrder.FilledQuantity, aggressorOrder.RemainingQuantity(), aggressorOrder.Status)
		me.debugLog("    [FIFO #%d] After fill - Resting: Filled=%s, Remaining=%s, Status=%v",
			orderIndex, restingOrder.FilledQuantity, restingOrder.RemainingQuantity(), restingOrder.Status)

		if me.IsDebugMode() {
			debugInfo := &TradeDebugInfo{
				TradeID:           trade.TradeID,
				ExecutionTime:     trade.Timestamp,
				ExecutionPrice:    tradePrice,
				ExecutionQuantity: matchQty,

				BuyOrderID:         buyOrder.ID,
				BuyerSide:          buyOrder.Side,
				BuyerLimitPrice:    buyOrder.Price,
				BuyerTotalQty:      buyOrder.Quantity,
				BuyerFilledBefore:  buyerFilledBefore,
				BuyerFilledAfter:   buyerFilledAfter,
				BuyerRemainingQty:  buyOrder.RemainingQuantity(),
				BuyerStatusBefore:  buyerStatusBefore,
				BuyerStatusAfter:   buyerStatusAfter,
				IsBuyerPartialFill: trade.IsBuyerPartialFill,

				SellOrderID:         sellOrder.ID,
				SellerSide:          sellOrder.Side,
				SellerLimitPrice:    sellOrder.Price,
				SellerTotalQty:      sellOrder.Quantity,
				SellerFilledBefore:  sellerFilledBefore,
				SellerFilledAfter:   sellerFilledAfter,
				SellerRemainingQty:  sellOrder.RemainingQuantity(),
				SellerStatusBefore:  sellerStatusBefore,
				SellerStatusAfter:   sellerStatusAfter,
				IsSellerPartialFill: trade.IsSellerPartialFill,

				MatchLevel:   matchLevel,
				FIFOPosition: orderIndex,
			}

			me.mu.Lock()
			me.tradeDebugInfo = append(me.tradeDebugInfo, debugInfo)
			me.mu.Unlock()

			me.debugLog("\n%s\n", debugInfo.String())
		}

		me.orderBook.UpdateOrder(restingOrder.ID.String(), restingOrder.FilledQuantity)

		if restingOrder.IsFilled() {
			me.debugLog("    [FIFO #%d] Resting order fully filled, removing from book", orderIndex)
			removedOrder := me.orderBook.RemoveOrder(restingOrder.ID.String())

			if removedOrder != nil {
				me.updateOrderbookMetrics(removedOrder.Instrument)
			}

			if removedOrder != nil {
				side := "buy"
				if removedOrder.Side == models.OrderSideSell {
					side = "sell"
				}
				me.publishOrderbookChange(side, "remove", removedOrder.Price, decimal.Zero, removedOrder.RemainingQuantity())
			}
		}

		element = nextElement
	}

	me.debugLog("  Price level matching complete: %d trades created", len(trades))

	return trades
}

func (me *MatchingEngine) GetOrderBook() *OrderBook {
	return me.orderBook
}

func (me *MatchingEngine) GetStats() map[string]interface{} {
	me.mu.RLock()
	defer me.mu.RUnlock()

	return map[string]interface{}{
		"is_running":          me.IsRunning(),
		"total_orders":        me.orderBook.Size(),
		"command_backlog":     len(me.commandChan),
		"command_capacity":    cap(me.commandChan),
		"channel_utilization": fmt.Sprintf("%.1f%%", float64(len(me.commandChan))/float64(cap(me.commandChan))*100),
		"bid_levels":          me.orderBook.Bids.Len(),
		"ask_levels":          me.orderBook.Asks.Len(),
		"commands_processed":  me.commandsProcessed,
		"channel_blocks":      me.channelBlocks,
		"debug_mode":          me.debugMode,
	}
}

// InspectChannelStatus returns detailed channel status for debugging blocked channels
// Use this if commands are not being processed or the channel appears blocked
func (me *MatchingEngine) InspectChannelStatus() map[string]interface{} {
	channelLen := len(me.commandChan)
	channelCap := cap(me.commandChan)

	status := map[string]interface{}{
		"channel_length":      channelLen,
		"channel_capacity":    channelCap,
		"channel_free_slots":  channelCap - channelLen,
		"channel_utilization": fmt.Sprintf("%.2f%%", float64(channelLen)/float64(channelCap)*100),
		"is_full":             channelLen >= channelCap,
		"is_near_full":        channelLen >= channelCap-10,
	}

	me.mu.RLock()
	status["worker_running"] = me.isRunning
	status["commands_processed"] = me.commandsProcessed
	status["channel_blocks"] = me.channelBlocks
	me.mu.RUnlock()

	// Diagnostic recommendations
	if channelLen >= channelCap {
		status["diagnostic"] = "CRITICAL: Channel is FULL! Consumer goroutine may be blocked or stopped."
		status["recommendation"] = "Check if matchingWorker is running. Call IsRunning() or check goroutine stack traces."
	} else if channelLen >= channelCap-10 {
		status["diagnostic"] = "WARNING: Channel is nearly full."
		status["recommendation"] = "Consider increasing buffer size or reducing submission rate."
	} else {
		status["diagnostic"] = "OK: Channel has sufficient capacity."
	}

	return status
}

// updateOrderbookMetrics updates Prometheus metrics for orderbook state
func (me *MatchingEngine) updateOrderbookMetrics(instrument string) {
	// Get current orderbook state
	bidDepth := me.orderBook.GetBidDepth()
	askDepth := me.orderBook.GetAskDepth()

	// Update depth gauges
	metrics.UpdateOrderbookDepth(instrument, "buy", float64(bidDepth))
	metrics.UpdateOrderbookDepth(instrument, "sell", float64(askDepth))

	// Update best prices
	bestBid := me.orderBook.GetBestBidPrice()
	bestAsk := me.orderBook.GetBestAskPrice()

	bestBidPrice := 0.0
	bestAskPrice := 0.0

	if bestBid != nil {
		bestBidPrice, _ = bestBid.Float64()
	}
	if bestAsk != nil {
		bestAskPrice, _ = bestAsk.Float64()
	}

	metrics.UpdateBestPrices(instrument, bestBidPrice, bestAskPrice)
}
