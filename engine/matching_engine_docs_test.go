package engine

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNewMatchingEngine(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")

	if me.orderBook.Instrument != "BTC-USD" {
		t.Errorf("Expected instrument BTC-USD, got %s", me.orderBook.Instrument)
	}

	if me.IsRunning() {
		t.Error("Expected matching engine to not be running initially")
	}
}

func TestMatchingEngineStartStop(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()

	// Start the engine
	err := me.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start matching engine: %v", err)
	}

	if !me.IsRunning() {
		t.Error("Expected matching engine to be running")
	}

	// Try to start again (should fail)
	err = me.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already running engine")
	}

	// Stop the engine
	err = me.Stop()
	if err != nil {
		t.Fatalf("Failed to stop matching engine: %v", err)
	}

	if me.IsRunning() {
		t.Error("Expected matching engine to be stopped")
	}

	// Try to stop again (should fail)
	err = me.Stop()
	if err == nil {
		t.Error("Expected error when stopping already stopped engine")
	}
}

func TestMatchLimitOrderBuyAgainstAsk(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Add a sell order to the book
	sellOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(sellOrder)
	if err != nil {
		t.Fatalf("Failed to submit sell order: %v", err)
	}

	if len(response.Trades) != 0 {
		t.Error("Expected no trades for first order")
	}

	// Give worker time to process
	time.Sleep(10 * time.Millisecond)

	// Submit a buy order that matches
	buyOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err = me.SubmitOrder(buyOrder)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	if len(response.Trades) != 1 {
		t.Fatalf("Expected 1 trade, got %d", len(response.Trades))
	}

	trade := response.Trades[0]
	if !trade.Price.Equal(decimal.NewFromFloat(50000.0)) {
		t.Errorf("Expected trade price 50000.0, got %s", trade.Price)
	}

	if !trade.Quantity.Equal(decimal.NewFromFloat(1.0)) {
		t.Errorf("Expected trade quantity 1.0, got %s", trade.Quantity)
	}

	if trade.BuyOrderID != buyOrder.ID {
		t.Error("Trade buy order ID mismatch")
	}

	if trade.SellOrderID != sellOrder.ID {
		t.Error("Trade sell order ID mismatch")
	}

	// Give worker time to process
	time.Sleep(10 * time.Millisecond)

	// Order book should be empty
	if me.orderBook.Size() != 0 {
		t.Errorf("Expected empty order book, got size %d", me.orderBook.Size())
	}
}

func TestMatchLimitOrderPartialFill(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Add a large sell order
	sellOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sellOrder)
	time.Sleep(10 * time.Millisecond)

	// Submit a smaller buy order
	buyOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(buyOrder)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	if len(response.Trades) != 1 {
		t.Fatalf("Expected 1 trade, got %d", len(response.Trades))
	}

	// Trade should be for 2.0 (the smaller quantity)
	if !response.Trades[0].Quantity.Equal(decimal.NewFromFloat(2.0)) {
		t.Errorf("Expected trade quantity 2.0, got %s", response.Trades[0].Quantity)
	}

	// Buy order should be filled
	if response.Order.Status != models.OrderStatusFilled {
		t.Errorf("Expected buy order status Filled, got %v", response.Order.Status)
	}

	time.Sleep(10 * time.Millisecond)

	// Sell order should still be in the book with 3.0 remaining
	if me.orderBook.Size() != 1 {
		t.Errorf("Expected 1 order in book, got %d", me.orderBook.Size())
	}
}

func TestMatchLimitOrderFIFO(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Add three sell orders at the same price in sequence
	sell1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell3 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell1)
	_, _ = me.SubmitOrder(sell2)
	_, _ = me.SubmitOrder(sell3)
	time.Sleep(20 * time.Millisecond)

	// Submit a buy order that should match all three
	buyOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(buyOrder)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	if len(response.Trades) != 3 {
		t.Fatalf("Expected 3 trades, got %d", len(response.Trades))
	}

	// Verify FIFO order: sell1 matched first, then sell2, then sell3
	if response.Trades[0].SellOrderID != sell1.ID {
		t.Error("Expected first trade to match sell1 (FIFO)")
	}
	if response.Trades[1].SellOrderID != sell2.ID {
		t.Error("Expected second trade to match sell2 (FIFO)")
	}
	if response.Trades[2].SellOrderID != sell3.ID {
		t.Error("Expected third trade to match sell3 (FIFO)")
	}
}

func TestMatchLimitOrderPriceTimePriority(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Add sell orders at different prices
	sell1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50100.0), // Higher price
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0), // Lower price (better)
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell1)
	_, _ = me.SubmitOrder(sell2)
	time.Sleep(20 * time.Millisecond)

	// Submit a buy order that can match both
	buyOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50200.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(buyOrder)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	if len(response.Trades) != 1 {
		t.Fatalf("Expected 1 trade, got %d", len(response.Trades))
	}

	// Should match with sell2 (better price)
	if response.Trades[0].SellOrderID != sell2.ID {
		t.Error("Expected to match with lower priced sell order (price priority)")
	}

	// Trade should execute at the resting order's price (50000.0)
	if !response.Trades[0].Price.Equal(decimal.NewFromFloat(50000.0)) {
		t.Errorf("Expected trade at resting price 50000.0, got %s", response.Trades[0].Price)
	}
}

func TestMatchMarketOrder(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Add sell orders at different prices
	sell1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50100.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell1)
	_, _ = me.SubmitOrder(sell2)
	time.Sleep(20 * time.Millisecond)

	// Submit a market buy order
	marketBuy := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeMarket,
		Price:          decimal.Zero, // Market orders don't have a price
		Quantity:       decimal.NewFromFloat(2.5),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(marketBuy)
	if err != nil {
		t.Fatalf("Failed to submit market order: %v", err)
	}

	// Should create 2 trades (one at 50000, one at 50100)
	if len(response.Trades) != 2 {
		t.Fatalf("Expected 2 trades, got %d", len(response.Trades))
	}

	// First trade at 50000.0 for 1.0
	if !response.Trades[0].Price.Equal(decimal.NewFromFloat(50000.0)) {
		t.Errorf("Expected first trade at 50000.0, got %s", response.Trades[0].Price)
	}
	if !response.Trades[0].Quantity.Equal(decimal.NewFromFloat(1.0)) {
		t.Errorf("Expected first trade quantity 1.0, got %s", response.Trades[0].Quantity)
	}

	// Second trade at 50100.0 for 1.5
	if !response.Trades[1].Price.Equal(decimal.NewFromFloat(50100.0)) {
		t.Errorf("Expected second trade at 50100.0, got %s", response.Trades[1].Price)
	}
	if !response.Trades[1].Quantity.Equal(decimal.NewFromFloat(1.5)) {
		t.Errorf("Expected second trade quantity 1.5, got %s", response.Trades[1].Quantity)
	}
}

func TestMatchMarketOrderNoLiquidity(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Submit a market order with no opposing orders
	marketBuy := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeMarket,
		Price:          decimal.Zero,
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(marketBuy)
	if err != nil {
		t.Fatalf("Failed to submit market order: %v", err)
	}

	// Should have no trades
	if len(response.Trades) != 0 {
		t.Errorf("Expected no trades, got %d", len(response.Trades))
	}

	// Order should be rejected
	if response.Order.Status != models.OrderStatusRejected {
		t.Errorf("Expected order status Rejected, got %v", response.Order.Status)
	}
}

func TestMatchingEngineCancelOrder(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Add an order
	order := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(order)
	time.Sleep(10 * time.Millisecond)

	if me.orderBook.Size() != 1 {
		t.Errorf("Expected 1 order in book, got %d", me.orderBook.Size())
	}

	// Cancel the order
	response, err := me.CancelOrder(order.ID)
	if err != nil {
		t.Fatalf("Failed to cancel order: %v", err)
	}

	if response.Order.Status != models.OrderStatusCancelled {
		t.Errorf("Expected order status Cancelled, got %v", response.Order.Status)
	}

	time.Sleep(10 * time.Millisecond)

	if me.orderBook.Size() != 0 {
		t.Errorf("Expected empty order book after cancel, got size %d", me.orderBook.Size())
	}
}

func TestConcurrentOrderSubmission(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Submit multiple orders concurrently (tests thread safety)
	orderCount := 100
	done := make(chan bool)

	for i := 0; i < orderCount; i++ {
		go func(idx int) {
			order := &models.Order{
				ID:             uuid.New(),
				ClientID:       "client",
				Instrument:     "BTC-USD",
				Side:           models.OrderSideBuy,
				Type:           models.OrderTypeLimit,
				Price:          decimal.NewFromFloat(50000.0 + float64(idx)),
				Quantity:       decimal.NewFromFloat(1.0),
				FilledQuantity: decimal.Zero,
				Status:         models.OrderStatusOpen,
			}
			_, _ = me.SubmitOrder(order)
			done <- true
		}(i)
	}

	// Wait for all submissions
	for i := 0; i < orderCount; i++ {
		<-done
	}

	// Give worker time to process all orders
	time.Sleep(100 * time.Millisecond)

	// All orders should be in the book
	if me.orderBook.Size() != orderCount {
		t.Errorf("Expected %d orders in book, got %d", orderCount, me.orderBook.Size())
	}
}

func TestTradeHandler(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Set up trade handler
	tradeCount := 0
	me.SetTradeHandler(func(trade *Trade) {
		tradeCount++
	})

	// Add a sell order
	sellOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sellOrder)
	time.Sleep(10 * time.Millisecond)

	// Add a buy order that matches
	buyOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(buyOrder)
	time.Sleep(10 * time.Millisecond)

	// Trade handler should have been called
	if tradeCount != 1 {
		t.Errorf("Expected trade handler to be called 1 time, got %d", tradeCount)
	}
}

func TestGetStats(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	stats := me.GetStats()

	if stats["is_running"] != true {
		t.Error("Expected is_running to be true")
	}

	if stats["total_orders"] != 0 {
		t.Error("Expected total_orders to be 0")
	}
}

func TestPartialFillScenario(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Step 1: Add a sell order for 10 shares at $100
	sellOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(10.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sellResponse, err := me.SubmitOrder(sellOrder)
	if err != nil {
		t.Fatalf("Failed to submit sell order: %v", err)
	}

	// Verify sell order was added to book (no trades yet)
	if len(sellResponse.Trades) != 0 {
		t.Errorf("Expected no trades for sell order, got %d", len(sellResponse.Trades))
	}

	// Give worker time to process
	time.Sleep(10 * time.Millisecond)

	// Verify sell order is in the book
	if me.orderBook.Size() != 1 {
		t.Fatalf("Expected 1 order in book, got %d", me.orderBook.Size())
	}

	// Step 2: Process a new buy order for 5 shares at $101
	buyOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(101.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	buyResponse, err := me.SubmitOrder(buyOrder)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	// Step 3: Verify that one trade for 5 shares at $100 is created
	if len(buyResponse.Trades) != 1 {
		t.Fatalf("Expected exactly 1 trade, got %d", len(buyResponse.Trades))
	}

	trade := buyResponse.Trades[0]

	// Verify trade quantity is 5 shares
	if !trade.Quantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected trade quantity 5.0, got %s", trade.Quantity)
	}

	// Verify trade price is $100 (resting order price, not aggressor price)
	if !trade.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected trade price 100.0, got %s", trade.Price)
	}

	// Verify trade IDs match
	if trade.BuyOrderID != buyOrder.ID {
		t.Error("Trade buy order ID does not match buy order")
	}

	if trade.SellOrderID != sellOrder.ID {
		t.Error("Trade sell order ID does not match sell order")
	}

	// Verify buy order is completely filled
	if buyResponse.Order.Status != models.OrderStatusFilled {
		t.Errorf("Expected buy order status Filled, got %v", buyResponse.Order.Status)
	}

	if !buyResponse.Order.FilledQuantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected buy order filled quantity 5.0, got %s", buyResponse.Order.FilledQuantity)
	}

	// Give worker time to process
	time.Sleep(10 * time.Millisecond)

	// Step 4: Check that the original sell order now has a remaining quantity of 5 shares
	remainingSellOrder := me.orderBook.GetOrder(sellOrder.ID.String())
	if remainingSellOrder == nil {
		t.Fatal("Expected sell order to still be in the order book")
	}

	// Verify the sell order has 5 shares filled
	if !remainingSellOrder.FilledQuantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected sell order filled quantity 5.0, got %s", remainingSellOrder.FilledQuantity)
	}

	// Verify remaining quantity is 5 shares
	remainingQty := remainingSellOrder.RemainingQuantity()
	if !remainingQty.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected sell order remaining quantity 5.0, got %s", remainingQty)
	}

	// Verify sell order status is partially filled
	if remainingSellOrder.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("Expected sell order status PartiallyFilled, got %v", remainingSellOrder.Status)
	}

	// Verify only the partially filled sell order remains in the book
	if me.orderBook.Size() != 1 {
		t.Errorf("Expected 1 order remaining in book, got %d", me.orderBook.Size())
	}

	// Verify the sell order is still at the best ask
	bestAsk := me.orderBook.GetBestAsk()
	if bestAsk == nil {
		t.Fatal("Expected best ask to exist")
	}

	if !bestAsk.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected best ask price 100.0, got %s", bestAsk.Price)
	}

	// Verify the price level volume is 5 shares
	if !bestAsk.Volume.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected price level volume 5.0, got %s", bestAsk.Volume)
	}
}

func TestMarketBuyAcrossMultiplePrices(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Step 1: Add sell orders at $100, $101, and $102
	sell100 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell101 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(101.0),
		Quantity:       decimal.NewFromFloat(4.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell102 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(102.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	// Submit all sell orders
	_, _ = me.SubmitOrder(sell100)
	_, _ = me.SubmitOrder(sell101)
	_, _ = me.SubmitOrder(sell102)

	// Give worker time to process
	time.Sleep(30 * time.Millisecond)

	// Verify all orders are in the book
	if me.orderBook.Size() != 3 {
		t.Fatalf("Expected 3 orders in book, got %d", me.orderBook.Size())
	}

	// Verify price levels
	bestAsk := me.orderBook.GetBestAsk()
	if bestAsk == nil || !bestAsk.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Error("Best ask should be $100")
	}

	// Step 2: Process a new market buy order for 10 shares
	marketBuy := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeMarket,
		Price:          decimal.Zero, // Market orders don't have a limit price
		Quantity:       decimal.NewFromFloat(10.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(marketBuy)
	if err != nil {
		t.Fatalf("Failed to submit market buy order: %v", err)
	}

	// Step 3: Verify it fills against sell orders starting from best price ($100) upwards

	// Should create 3 trades (one at each price level)
	if len(response.Trades) != 3 {
		t.Fatalf("Expected 3 trades, got %d", len(response.Trades))
	}

	// Trade 1: Should be at $100 for 3.0 shares (entire sell100 order)
	trade1 := response.Trades[0]
	if !trade1.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected first trade at $100, got %s", trade1.Price)
	}
	if !trade1.Quantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected first trade quantity 3.0, got %s", trade1.Quantity)
	}
	if trade1.SellOrderID != sell100.ID {
		t.Error("First trade should match sell100 order")
	}
	if trade1.BuyOrderID != marketBuy.ID {
		t.Error("First trade should match market buy order")
	}

	// Trade 2: Should be at $101 for 4.0 shares (entire sell101 order)
	trade2 := response.Trades[1]
	if !trade2.Price.Equal(decimal.NewFromFloat(101.0)) {
		t.Errorf("Expected second trade at $101, got %s", trade2.Price)
	}
	if !trade2.Quantity.Equal(decimal.NewFromFloat(4.0)) {
		t.Errorf("Expected second trade quantity 4.0, got %s", trade2.Quantity)
	}
	if trade2.SellOrderID != sell101.ID {
		t.Error("Second trade should match sell101 order")
	}
	if trade2.BuyOrderID != marketBuy.ID {
		t.Error("Second trade should match market buy order")
	}

	// Trade 3: Should be at $102 for 3.0 shares (partial fill of sell102 order)
	trade3 := response.Trades[2]
	if !trade3.Price.Equal(decimal.NewFromFloat(102.0)) {
		t.Errorf("Expected third trade at $102, got %s", trade3.Price)
	}
	if !trade3.Quantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected third trade quantity 3.0, got %s", trade3.Quantity)
	}
	if trade3.SellOrderID != sell102.ID {
		t.Error("Third trade should match sell102 order")
	}
	if trade3.BuyOrderID != marketBuy.ID {
		t.Error("Third trade should match market buy order")
	}

	// Verify total quantity traded
	totalTraded := trade1.Quantity.Add(trade2.Quantity).Add(trade3.Quantity)
	if !totalTraded.Equal(decimal.NewFromFloat(10.0)) {
		t.Errorf("Expected total traded 10.0, got %s", totalTraded)
	}

	// Verify market buy order is completely filled
	if response.Order.Status != models.OrderStatusFilled {
		t.Errorf("Expected market order status Filled, got %v", response.Order.Status)
	}
	if !response.Order.FilledQuantity.Equal(decimal.NewFromFloat(10.0)) {
		t.Errorf("Expected market order filled quantity 10.0, got %s", response.Order.FilledQuantity)
	}

	// Give worker time to process
	time.Sleep(20 * time.Millisecond)

	// Verify order book state after matching
	// sell100 and sell101 should be completely filled and removed
	// sell102 should still be in the book with 2.0 remaining (5.0 - 3.0)

	if me.orderBook.Size() != 1 {
		t.Errorf("Expected 1 order remaining in book, got %d", me.orderBook.Size())
	}

	// Best ask should now be $102
	bestAsk = me.orderBook.GetBestAsk()
	if bestAsk == nil {
		t.Fatal("Expected best ask to exist")
	}
	if !bestAsk.Price.Equal(decimal.NewFromFloat(102.0)) {
		t.Errorf("Expected best ask $102 after matching, got %s", bestAsk.Price)
	}

	// Volume at $102 should be 2.0 (5.0 - 3.0 traded)
	if !bestAsk.Volume.Equal(decimal.NewFromFloat(2.0)) {
		t.Errorf("Expected volume 2.0 at $102, got %s", bestAsk.Volume)
	}

	// Verify the order progression: $100 → $101 → $102
	t.Logf("✓ Market buy filled progressively:")
	t.Logf("  Trade 1: %s shares @ $%s (sell100)", trade1.Quantity, trade1.Price)
	t.Logf("  Trade 2: %s shares @ $%s (sell101)", trade2.Quantity, trade2.Price)
	t.Logf("  Trade 3: %s shares @ $%s (sell102 partial)", trade3.Quantity, trade3.Price)
	t.Logf("  Total: %s shares filled", totalTraded)
	t.Logf("  Remaining in book: %s shares @ $102", bestAsk.Volume)
}

// TestLimitOrderBuyCrossing tests buy limit order matching against asks with price <= buy.price
func TestLimitOrderBuyCrossing(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Setup: Add sell orders at different prices
	sell98 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(98.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell100 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell102 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(102.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell98)
	_, _ = me.SubmitOrder(sell100)
	_, _ = me.SubmitOrder(sell102)
	time.Sleep(30 * time.Millisecond)

	// Test 1: Buy at $99 - should match ONLY sell98 (98 <= 99), not others
	buyAt99 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(99.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(buyAt99)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	// Should generate 1 trade (with sell98 only)
	if len(response.Trades) != 1 {
		t.Fatalf("Expected 1 trade, got %d", len(response.Trades))
	}

	// Trade should be at $98 (resting order price) for 5.0 shares
	if !response.Trades[0].Price.Equal(decimal.NewFromFloat(98.0)) {
		t.Errorf("Expected trade at $98, got %s", response.Trades[0].Price)
	}
	if !response.Trades[0].Quantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected trade quantity 5.0, got %s", response.Trades[0].Quantity)
	}

	// Buy order should be completely filled
	if response.Order.Status != models.OrderStatusFilled {
		t.Errorf("Expected buy order filled, got %v", response.Order.Status)
	}

	time.Sleep(20 * time.Millisecond)

	// Verify: sell100 and sell102 should still be in book
	if me.orderBook.Size() != 2 {
		t.Errorf("Expected 2 orders in book, got %d", me.orderBook.Size())
	}

	t.Logf("✓ Buy limit at $99 matched only asks <= $99")

	// Test 2: Buy at $101 - should match sell100 (100 <= 101), not sell102
	buyAt101 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(101.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err = me.SubmitOrder(buyAt101)
	if err != nil {
		t.Fatalf("Failed to submit second buy order: %v", err)
	}

	// Should match sell100
	if len(response.Trades) != 1 {
		t.Fatalf("Expected 1 trade, got %d", len(response.Trades))
	}
	if !response.Trades[0].Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected trade at $100, got %s", response.Trades[0].Price)
	}

	time.Sleep(20 * time.Millisecond)

	// Only sell102 should remain
	if me.orderBook.Size() != 1 {
		t.Errorf("Expected 1 order in book, got %d", me.orderBook.Size())
	}

	bestAsk := me.orderBook.GetBestAsk()
	if bestAsk == nil || !bestAsk.Price.Equal(decimal.NewFromFloat(102.0)) {
		t.Error("Expected best ask to be $102")
	}

	t.Logf("✓ Buy limit at $101 matched only asks <= $101")
}

// TestLimitOrderSellCrossing tests sell limit order matching against bids with price >= sell.price
func TestLimitOrderSellCrossing(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Setup: Add buy orders at different prices
	bid105 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(105.0),
		Quantity:       decimal.NewFromFloat(4.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	bid103 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(103.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	bid100 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(bid105)
	_, _ = me.SubmitOrder(bid103)
	_, _ = me.SubmitOrder(bid100)
	time.Sleep(30 * time.Millisecond)

	// Test 1: Sell at $104 - should match ONLY bid105 (105 >= 104), not others
	sellAt104 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(104.0),
		Quantity:       decimal.NewFromFloat(4.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(sellAt104)
	if err != nil {
		t.Fatalf("Failed to submit sell order: %v", err)
	}

	// Should generate 1 trade (with bid105 only)
	if len(response.Trades) != 1 {
		t.Fatalf("Expected 1 trade, got %d", len(response.Trades))
	}

	// Trade should be at $105 (resting order price) for 4.0 shares
	if !response.Trades[0].Price.Equal(decimal.NewFromFloat(105.0)) {
		t.Errorf("Expected trade at $105, got %s", response.Trades[0].Price)
	}
	if !response.Trades[0].Quantity.Equal(decimal.NewFromFloat(4.0)) {
		t.Errorf("Expected trade quantity 4.0, got %s", response.Trades[0].Quantity)
	}

	// Sell order should be completely filled
	if response.Order.Status != models.OrderStatusFilled {
		t.Errorf("Expected sell order filled, got %v", response.Order.Status)
	}

	time.Sleep(20 * time.Millisecond)

	// Verify: bid103 and bid100 should still be in book
	if me.orderBook.Size() != 2 {
		t.Errorf("Expected 2 orders in book, got %d", me.orderBook.Size())
	}

	t.Logf("✓ Sell limit at $104 matched only bids >= $104")

	// Test 2: Sell at $102 - should match bid103 (103 >= 102), not bid100
	sellAt102 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(102.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err = me.SubmitOrder(sellAt102)
	if err != nil {
		t.Fatalf("Failed to submit second sell order: %v", err)
	}

	// Should match bid103
	if len(response.Trades) != 1 {
		t.Fatalf("Expected 1 trade, got %d", len(response.Trades))
	}
	if !response.Trades[0].Price.Equal(decimal.NewFromFloat(103.0)) {
		t.Errorf("Expected trade at $103, got %s", response.Trades[0].Price)
	}

	time.Sleep(20 * time.Millisecond)

	// Only bid100 should remain
	if me.orderBook.Size() != 1 {
		t.Errorf("Expected 1 order in book, got %d", me.orderBook.Size())
	}

	bestBid := me.orderBook.GetBestBid()
	if bestBid == nil || !bestBid.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Error("Expected best bid to be $100")
	}

	t.Logf("✓ Sell limit at $102 matched only bids >= $102")
}

// TestLimitOrderPartialFillWithLeftoverInBook tests partial fills where remainder goes into book
func TestLimitOrderPartialFillWithLeftoverInBook(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Setup: Add small sell order
	sell100 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell100)
	time.Sleep(20 * time.Millisecond)

	// Buy limit for more than available - should partial fill and rest in book
	buyAt102 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(102.0),
		Quantity:       decimal.NewFromFloat(10.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(buyAt102)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	// Should generate 1 trade for 3.0 shares (all of sell100)
	if len(response.Trades) != 1 {
		t.Fatalf("Expected 1 trade, got %d", len(response.Trades))
	}
	if !response.Trades[0].Quantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected trade quantity 3.0, got %s", response.Trades[0].Quantity)
	}

	// Buy order should be partially filled
	if response.Order.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("Expected buy order partially filled, got %v", response.Order.Status)
	}
	if !response.Order.FilledQuantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected filled quantity 3.0, got %s", response.Order.FilledQuantity)
	}
	if !response.Order.RemainingQuantity().Equal(decimal.NewFromFloat(7.0)) {
		t.Errorf("Expected remaining quantity 7.0, got %s", response.Order.RemainingQuantity())
	}

	time.Sleep(20 * time.Millisecond)

	// The remaining 7.0 shares should be in the order book as a bid
	if me.orderBook.Size() != 1 {
		t.Errorf("Expected 1 order in book (buy remainder), got %d", me.orderBook.Size())
	}

	bestBid := me.orderBook.GetBestBid()
	if bestBid == nil {
		t.Fatal("Expected best bid to exist")
	}
	if !bestBid.Price.Equal(decimal.NewFromFloat(102.0)) {
		t.Errorf("Expected best bid at $102, got %s", bestBid.Price)
	}
	if !bestBid.Volume.Equal(decimal.NewFromFloat(7.0)) {
		t.Errorf("Expected bid volume 7.0, got %s", bestBid.Volume)
	}

	// Verify the order in the book
	orderInBook := me.orderBook.GetOrder(buyAt102.ID.String())
	if orderInBook == nil {
		t.Fatal("Expected buy order to be in book")
	}
	if !orderInBook.FilledQuantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected order in book to have filled quantity 3.0, got %s", orderInBook.FilledQuantity)
	}

	t.Logf("✓ Partial fill: 3.0 matched, 7.0 remainder added to book")
}

// TestLimitOrderNoCrossing tests limit orders that don't cross the spread
func TestLimitOrderNoCrossing(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Setup: Create a spread
	sell110 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(110.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	bid90 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(90.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell110)
	_, _ = me.SubmitOrder(bid90)
	time.Sleep(20 * time.Millisecond)

	// Spread is $110 - $90 = $20

	// Test 1: Buy at $100 - doesn't cross (100 < 110), should go straight to book
	buyAt100 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(buyAt100)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	// Should generate NO trades
	if len(response.Trades) != 0 {
		t.Fatalf("Expected 0 trades, got %d", len(response.Trades))
	}

	// Order should be open (not filled at all)
	if response.Order.Status != models.OrderStatusOpen {
		t.Errorf("Expected order status Open, got %v", response.Order.Status)
	}
	if !response.Order.FilledQuantity.Equal(decimal.Zero) {
		t.Errorf("Expected filled quantity 0, got %s", response.Order.FilledQuantity)
	}

	time.Sleep(20 * time.Millisecond)

	// Should be in the book
	if me.orderBook.Size() != 3 {
		t.Errorf("Expected 3 orders in book, got %d", me.orderBook.Size())
	}

	t.Logf("✓ Buy at $100 didn't cross sell at $110, added to book")

	// Test 2: Sell at $105 - doesn't cross (105 > 100), should go straight to book
	sellAt105 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(105.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err = me.SubmitOrder(sellAt105)
	if err != nil {
		t.Fatalf("Failed to submit sell order: %v", err)
	}

	// Should generate NO trades
	if len(response.Trades) != 0 {
		t.Fatalf("Expected 0 trades, got %d", len(response.Trades))
	}

	// Order should be open
	if response.Order.Status != models.OrderStatusOpen {
		t.Errorf("Expected order status Open, got %v", response.Order.Status)
	}

	time.Sleep(20 * time.Millisecond)

	// All 4 orders should be in book
	if me.orderBook.Size() != 4 {
		t.Errorf("Expected 4 orders in book, got %d", me.orderBook.Size())
	}

	// Verify the spread still exists
	bestBid := me.orderBook.GetBestBid()
	bestAsk := me.orderBook.GetBestAsk()

	if bestBid == nil || bestAsk == nil {
		t.Fatal("Expected both best bid and best ask to exist")
	}

	// Best bid should be $100, best ask should be $105
	if !bestBid.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected best bid at $100, got %s", bestBid.Price)
	}
	if !bestAsk.Price.Equal(decimal.NewFromFloat(105.0)) {
		t.Errorf("Expected best ask at $105, got %s", bestAsk.Price)
	}

	t.Logf("✓ Sell at $105 didn't cross buy at $100, added to book")
}

// TestLimitOrderMultipleLevelCrossing tests limit order matching across multiple price levels
func TestLimitOrderMultipleLevelCrossing(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()
	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Setup: Add multiple sell orders at different prices
	sell100 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell101 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(101.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell103 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(103.0),
		Quantity:       decimal.NewFromFloat(4.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell100)
	_, _ = me.SubmitOrder(sell101)
	_, _ = me.SubmitOrder(sell103)
	time.Sleep(30 * time.Millisecond)

	// Buy limit at $102 - should match sell100 (100 <= 102) and sell101 (101 <= 102)
	// but NOT sell103 (103 > 102)
	buyAt102 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(102.0),
		Quantity:       decimal.NewFromFloat(10.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(buyAt102)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	// Should generate 2 trades (sell100 and sell101, but not sell103)
	if len(response.Trades) != 2 {
		t.Fatalf("Expected 2 trades, got %d", len(response.Trades))
	}

	// Trade 1: 2.0 shares at $100
	if !response.Trades[0].Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected first trade at $100, got %s", response.Trades[0].Price)
	}
	if !response.Trades[0].Quantity.Equal(decimal.NewFromFloat(2.0)) {
		t.Errorf("Expected first trade quantity 2.0, got %s", response.Trades[0].Quantity)
	}

	// Trade 2: 3.0 shares at $101
	if !response.Trades[1].Price.Equal(decimal.NewFromFloat(101.0)) {
		t.Errorf("Expected second trade at $101, got %s", response.Trades[1].Price)
	}
	if !response.Trades[1].Quantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected second trade quantity 3.0, got %s", response.Trades[1].Quantity)
	}

	// Total matched: 5.0 shares, remaining: 5.0 shares
	if !response.Order.FilledQuantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected filled quantity 5.0, got %s", response.Order.FilledQuantity)
	}
	if !response.Order.RemainingQuantity().Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected remaining quantity 5.0, got %s", response.Order.RemainingQuantity())
	}

	// Order should be partially filled
	if response.Order.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("Expected order status PartiallyFilled, got %v", response.Order.Status)
	}

	time.Sleep(20 * time.Millisecond)

	// Book should have 2 orders: sell103 (untouched) and buyAt102 remainder (5.0 shares)
	if me.orderBook.Size() != 2 {
		t.Errorf("Expected 2 orders in book, got %d", me.orderBook.Size())
	}

	// Best ask should be $103
	bestAsk := me.orderBook.GetBestAsk()
	if bestAsk == nil || !bestAsk.Price.Equal(decimal.NewFromFloat(103.0)) {
		t.Error("Expected best ask at $103")
	}

	// Best bid should be $102 with 5.0 volume (remainder)
	bestBid := me.orderBook.GetBestBid()
	if bestBid == nil || !bestBid.Price.Equal(decimal.NewFromFloat(102.0)) {
		t.Error("Expected best bid at $102")
	}
	if !bestBid.Volume.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected bid volume 5.0, got %s", bestBid.Volume)
	}

	t.Logf("✓ Buy limit at $102 matched 2 levels (100, 101), stopped at 103, added remainder to book")
}

// TestPartialFillDebugging tests detailed logging and persistence of partial fill states
func TestPartialFillDebugging(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()

	// Enable debug mode and capture output
	var buf bytes.Buffer
	logger := log.New(&buf, "", log.Lmicroseconds)
	me.SetDebugLogger(logger)
	me.EnableDebugMode(true)

	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Setup: Add multiple sell orders at same price
	sell1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(8.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell1)
	_, _ = me.SubmitOrder(sell2)
	time.Sleep(30 * time.Millisecond)

	// Buy order that will create partial fills
	buy := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(101.0),
		Quantity:       decimal.NewFromFloat(10.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(buy)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	// Should generate 2 trades
	if len(response.Trades) != 2 {
		t.Fatalf("Expected 2 trades, got %d", len(response.Trades))
	}

	// Trade 1: Buy 5.0 from sell1 (sell1 fully filled)
	trade1 := response.Trades[0]
	if !trade1.Quantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected trade1 quantity 5.0, got %s", trade1.Quantity)
	}
	if trade1.IsSellerPartialFill {
		t.Error("Sell1 should be fully filled, not partial")
	}
	if !trade1.IsBuyerPartialFill {
		t.Error("Buyer should be partially filled after first trade")
	}

	// Verify trade1 state info
	if !trade1.BuyerFilledQty.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected buyer filled 5.0 after trade1, got %s", trade1.BuyerFilledQty)
	}
	if !trade1.BuyerRemainingQty.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected buyer remaining 5.0 after trade1, got %s", trade1.BuyerRemainingQty)
	}
	if !trade1.SellerFilledQty.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected seller filled 5.0 after trade1, got %s", trade1.SellerFilledQty)
	}
	if !trade1.SellerRemainingQty.Equal(decimal.Zero) {
		t.Errorf("Expected seller remaining 0 after trade1, got %s", trade1.SellerRemainingQty)
	}

	// Trade 2: Buy 5.0 from sell2 (sell2 partially filled, 3.0 remaining)
	trade2 := response.Trades[1]
	if !trade2.Quantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected trade2 quantity 5.0, got %s", trade2.Quantity)
	}
	if !trade2.IsSellerPartialFill {
		t.Error("Sell2 should be partially filled")
	}
	if trade2.IsBuyerPartialFill {
		t.Error("Buyer should be fully filled after second trade")
	}

	// Verify trade2 state info
	if !trade2.BuyerFilledQty.Equal(decimal.NewFromFloat(10.0)) {
		t.Errorf("Expected buyer filled 10.0 after trade2, got %s", trade2.BuyerFilledQty)
	}
	if !trade2.BuyerRemainingQty.Equal(decimal.Zero) {
		t.Errorf("Expected buyer remaining 0 after trade2, got %s", trade2.BuyerRemainingQty)
	}
	if !trade2.SellerFilledQty.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected seller filled 5.0 after trade2, got %s", trade2.SellerFilledQty)
	}
	if !trade2.SellerRemainingQty.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected seller remaining 3.0 after trade2, got %s", trade2.SellerRemainingQty)
	}

	// Verify debug output contains sub-fill execution logs
	debugOutput := buf.String()

	if !strings.Contains(debugOutput, "SUB-FILL EXECUTION") {
		t.Error("Debug output should contain SUB-FILL EXECUTION logs")
	}

	if !strings.Contains(debugOutput, "PARTIAL FILL DETECTED") {
		t.Error("Debug output should contain PARTIAL FILL DETECTED logs")
	}

	// Verify both trade prices are logged
	if !strings.Contains(debugOutput, "Price=100") {
		t.Error("Debug output should contain execution price $100")
	}

	// Get debug info from engine
	debugInfo := me.GetTradeDebugInfo()
	if len(debugInfo) != 2 {
		t.Fatalf("Expected 2 debug info entries, got %d", len(debugInfo))
	}

	// Verify first trade debug info
	info1 := debugInfo[0]
	if !info1.ExecutionPrice.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected execution price $100, got %s", info1.ExecutionPrice)
	}
	if !info1.ExecutionQuantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected execution quantity 5.0, got %s", info1.ExecutionQuantity)
	}
	if info1.FIFOPosition != 1 {
		t.Errorf("Expected FIFO position 1, got %d", info1.FIFOPosition)
	}
	if info1.BuyerStatusBefore != models.OrderStatusOpen {
		t.Errorf("Expected buyer status before: Open, got %v", info1.BuyerStatusBefore)
	}
	if info1.BuyerStatusAfter != models.OrderStatusPartiallyFilled {
		t.Errorf("Expected buyer status after: PartiallyFilled, got %v", info1.BuyerStatusAfter)
	}
	if info1.SellerStatusAfter != models.OrderStatusFilled {
		t.Errorf("Expected seller status after: Filled, got %v", info1.SellerStatusAfter)
	}
	if !info1.IsBuyerPartialFill {
		t.Error("Expected buyer partial fill flag to be true")
	}

	// Verify second trade debug info
	info2 := debugInfo[1]
	if !info2.ExecutionPrice.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected execution price $100, got %s", info2.ExecutionPrice)
	}
	if !info2.ExecutionQuantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected execution quantity 5.0, got %s", info2.ExecutionQuantity)
	}
	if info2.FIFOPosition != 2 {
		t.Errorf("Expected FIFO position 2, got %d", info2.FIFOPosition)
	}
	if info2.BuyerStatusBefore != models.OrderStatusPartiallyFilled {
		t.Errorf("Expected buyer status before: PartiallyFilled, got %v", info2.BuyerStatusBefore)
	}
	if info2.BuyerStatusAfter != models.OrderStatusFilled {
		t.Errorf("Expected buyer status after: Filled, got %v", info2.BuyerStatusAfter)
	}
	if info2.SellerStatusAfter != models.OrderStatusPartiallyFilled {
		t.Errorf("Expected seller status after: PartiallyFilled, got %v", info2.SellerStatusAfter)
	}
	if !info2.IsSellerPartialFill {
		t.Error("Expected seller partial fill flag to be true")
	}

	t.Logf("✓ Partial fill debugging verified:")
	t.Logf("  Trade 1: Qty=%s @ $%s, Buyer: %s->%s, Seller: %s->%s",
		info1.ExecutionQuantity, info1.ExecutionPrice,
		info1.BuyerStatusBefore, info1.BuyerStatusAfter,
		info1.SellerStatusBefore, info1.SellerStatusAfter)
	t.Logf("  Trade 2: Qty=%s @ $%s, Buyer: %s->%s, Seller: %s->%s",
		info2.ExecutionQuantity, info2.ExecutionPrice,
		info2.BuyerStatusBefore, info2.BuyerStatusAfter,
		info2.SellerStatusBefore, info2.SellerStatusAfter)
}

// TestMultiLevelPartialFillDebugging tests partial fill debugging across multiple price levels
func TestMultiLevelPartialFillDebugging(t *testing.T) {
	me := NewMatchingEngine("BTC-USD")
	ctx := context.Background()

	var buf bytes.Buffer
	logger := log.New(&buf, "", log.Lmicroseconds)
	me.SetDebugLogger(logger)
	me.EnableDebugMode(true)

	defer func() { _ = me.Start(ctx) }()
	defer func() { _ = me.Stop() }()

	// Setup: Sells at different prices
	sell100 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	sell101 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(101.0),
		Quantity:       decimal.NewFromFloat(4.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	_, _ = me.SubmitOrder(sell100)
	_, _ = me.SubmitOrder(sell101)
	time.Sleep(30 * time.Millisecond)

	// Buy that will match across both levels with partial fill at second level
	buy := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(102.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}

	response, err := me.SubmitOrder(buy)
	if err != nil {
		t.Fatalf("Failed to submit buy order: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	// Should generate 2 trades at different prices
	if len(response.Trades) != 2 {
		t.Fatalf("Expected 2 trades, got %d", len(response.Trades))
	}

	// Verify debug info captured different prices
	debugInfo := me.GetTradeDebugInfo()
	if len(debugInfo) != 2 {
		t.Fatalf("Expected 2 debug info entries, got %d", len(debugInfo))
	}

	// First trade at $100
	if !debugInfo[0].ExecutionPrice.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected first trade at $100, got %s", debugInfo[0].ExecutionPrice)
	}
	if !debugInfo[0].ExecutionQuantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected first trade quantity 3.0, got %s", debugInfo[0].ExecutionQuantity)
	}

	// Second trade at $101 (partial fill)
	if !debugInfo[1].ExecutionPrice.Equal(decimal.NewFromFloat(101.0)) {
		t.Errorf("Expected second trade at $101, got %s", debugInfo[1].ExecutionPrice)
	}
	if !debugInfo[1].ExecutionQuantity.Equal(decimal.NewFromFloat(2.0)) {
		t.Errorf("Expected second trade quantity 2.0, got %s", debugInfo[1].ExecutionQuantity)
	}
	if !debugInfo[1].IsSellerPartialFill {
		t.Error("Expected seller partial fill at second level")
	}

	// Verify debug output logs execution at both prices
	debugOutput := buf.String()
	if !strings.Contains(debugOutput, "Price=100") {
		t.Error("Debug output should log execution at $100")
	}
	if !strings.Contains(debugOutput, "Price=101") {
		t.Error("Debug output should log execution at $101")
	}

	t.Logf("✓ Multi-level partial fill debugging verified:")
	t.Logf("  Level 1: %s @ $%s", debugInfo[0].ExecutionQuantity, debugInfo[0].ExecutionPrice)
	t.Logf("  Level 2: %s @ $%s (partial fill)", debugInfo[1].ExecutionQuantity, debugInfo[1].ExecutionPrice)
}
