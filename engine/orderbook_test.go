package engine

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourusername/trading-engine/models"
)

func TestNewOrderBook(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	if ob.Instrument != "BTC-USD" {
		t.Errorf("Expected instrument BTC-USD, got %s", ob.Instrument)
	}
	
	if ob.Size() != 0 {
		t.Errorf("Expected empty order book, got size %d", ob.Size())
	}
}

func TestAddOrderToBids(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	order := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.5),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(order)
	
	if ob.Size() != 1 {
		t.Errorf("Expected order book size 1, got %d", ob.Size())
	}
	
	retrieved := ob.GetOrder(order.ID.String())
	if retrieved == nil {
		t.Fatal("Failed to retrieve order from order book")
	}
	
	if !retrieved.Price.Equal(decimal.NewFromFloat(50000.0)) {
		t.Errorf("Expected price 50000.0, got %s", retrieved.Price)
	}
}

func TestAddOrderToAsks(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	order := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(51000.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(order)
	
	if ob.Size() != 1 {
		t.Errorf("Expected order book size 1, got %d", ob.Size())
	}
	
	bestAsk := ob.GetBestAsk()
	if bestAsk == nil {
		t.Fatal("Expected best ask to exist")
	}
	
	if !bestAsk.Price.Equal(decimal.NewFromFloat(51000.0)) {
		t.Errorf("Expected best ask price 51000.0, got %s", bestAsk.Price)
	}
}

func TestRemoveOrder(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
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
	
	ob.AddOrder(order)
	
	removed := ob.RemoveOrder(order.ID.String())
	if removed == nil {
		t.Fatal("Failed to remove order")
	}
	
	if ob.Size() != 0 {
		t.Errorf("Expected empty order book after removal, got size %d", ob.Size())
	}
	
	// Try to retrieve removed order
	retrieved := ob.GetOrder(order.ID.String())
	if retrieved != nil {
		t.Error("Order should not exist after removal")
	}
}

func TestGetBestBidAndAsk(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add multiple bid orders at different prices
	bid1 := &models.Order{
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
	
	bid2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50100.0), // Higher bid
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	// Add multiple ask orders at different prices
	ask1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(51000.0),
		Quantity:       decimal.NewFromFloat(1.5),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ask2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client4",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50900.0), // Lower ask
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(bid1)
	ob.AddOrder(bid2)
	ob.AddOrder(ask1)
	ob.AddOrder(ask2)
	
	// Best bid should be the highest price (50100.0)
	bestBid := ob.GetBestBid()
	if bestBid == nil {
		t.Fatal("Expected best bid to exist")
	}
	if !bestBid.Price.Equal(decimal.NewFromFloat(50100.0)) {
		t.Errorf("Expected best bid 50100.0, got %s", bestBid.Price)
	}
	
	// Best ask should be the lowest price (50900.0)
	bestAsk := ob.GetBestAsk()
	if bestAsk == nil {
		t.Fatal("Expected best ask to exist")
	}
	if !bestAsk.Price.Equal(decimal.NewFromFloat(50900.0)) {
		t.Errorf("Expected best ask 50900.0, got %s", bestAsk.Price)
	}
}

func TestGetSpread(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	bid := &models.Order{
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
	
	ask := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50100.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(bid)
	ob.AddOrder(ask)
	
	spread := ob.GetSpread()
	expectedSpread := decimal.NewFromFloat(100.0)
	
	if !spread.Equal(expectedSpread) {
		t.Errorf("Expected spread 100.0, got %s", spread)
	}
}

func TestPriceLevelFIFO(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add three orders at the same price
	order1 := &models.Order{
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
	
	order2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	order3 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(order1)
	ob.AddOrder(order2)
	ob.AddOrder(order3)
	
	// Verify all orders are at the same price level
	bestBid := ob.GetBestBid()
	if bestBid == nil {
		t.Fatal("Expected best bid to exist")
	}
	
	// Check that the price level has all three orders
	if bestBid.Orders.Len() != 3 {
		t.Errorf("Expected 3 orders at price level, got %d", bestBid.Orders.Len())
	}
	
	// Check total volume
	expectedVolume := decimal.NewFromFloat(6.0) // 1 + 2 + 3
	if !bestBid.Volume.Equal(expectedVolume) {
		t.Errorf("Expected volume 6.0, got %s", bestBid.Volume)
	}
	
	// Verify FIFO order
	element := bestBid.Orders.Front()
	firstOrder := element.Value.(*models.Order)
	if firstOrder.ID != order1.ID {
		t.Error("Expected first order to be order1 (FIFO)")
	}
	
	element = element.Next()
	secondOrder := element.Value.(*models.Order)
	if secondOrder.ID != order2.ID {
		t.Error("Expected second order to be order2 (FIFO)")
	}
	
	element = element.Next()
	thirdOrder := element.Value.(*models.Order)
	if thirdOrder.ID != order3.ID {
		t.Error("Expected third order to be order3 (FIFO)")
	}
}

func TestUpdateOrder(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	order := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(order)
	
	// Partially fill the order
	ob.UpdateOrder(order.ID.String(), decimal.NewFromFloat(2.0))
	
	retrieved := ob.GetOrder(order.ID.String())
	if retrieved == nil {
		t.Fatal("Failed to retrieve order")
	}
	
	if !retrieved.FilledQuantity.Equal(decimal.NewFromFloat(2.0)) {
		t.Errorf("Expected filled quantity 2.0, got %s", retrieved.FilledQuantity)
	}
	
	if retrieved.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("Expected status PartiallyFilled, got %v", retrieved.Status)
	}
	
	// Fully fill the order
	ob.UpdateOrder(order.ID.String(), decimal.NewFromFloat(5.0))
	
	retrieved = ob.GetOrder(order.ID.String())
	if retrieved.Status != models.OrderStatusFilled {
		t.Errorf("Expected status Filled, got %v", retrieved.Status)
	}
}

func TestGetTopLevels(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add multiple bid orders at different prices
	for i := 0; i < 5; i++ {
		price := 50000.0 + float64(i*100)
		order := &models.Order{
			ID:             uuid.New(),
			ClientID:       "client",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideBuy,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(price),
			Quantity:       decimal.NewFromFloat(1.0),
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
		}
		ob.AddOrder(order)
	}
	
	// Add multiple ask orders at different prices
	for i := 0; i < 5; i++ {
		price := 51000.0 + float64(i*100)
		order := &models.Order{
			ID:             uuid.New(),
			ClientID:       "client",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideSell,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(price),
			Quantity:       decimal.NewFromFloat(1.0),
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
		}
		ob.AddOrder(order)
	}
	
	// Get top 3 levels
	bids, asks := ob.GetTopLevels(3)
	
	if len(bids) != 3 {
		t.Errorf("Expected 3 bid levels, got %d", len(bids))
	}
	
	if len(asks) != 3 {
		t.Errorf("Expected 3 ask levels, got %d", len(asks))
	}
	
	// Verify bids are in descending order (highest first)
	if !bids[0].Price.Equal(decimal.NewFromFloat(50400.0)) {
		t.Errorf("Expected first bid 50400.0, got %s", bids[0].Price)
	}
	
	// Verify asks are in ascending order (lowest first)
	if !asks[0].Price.Equal(decimal.NewFromFloat(51000.0)) {
		t.Errorf("Expected first ask 51000.0, got %s", asks[0].Price)
	}
}

func TestGetDepth(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add bid orders
	bid1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	bid2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(49900.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	// Add ask orders
	ask1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(51000.0),
		Quantity:       decimal.NewFromFloat(1.5),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ask2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client4",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(51100.0),
		Quantity:       decimal.NewFromFloat(2.5),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(bid1)
	ob.AddOrder(bid2)
	ob.AddOrder(ask1)
	ob.AddOrder(ask2)
	
	bidVolume, askVolume := ob.GetDepth()
	
	expectedBidVolume := decimal.NewFromFloat(5.0) // 2 + 3
	expectedAskVolume := decimal.NewFromFloat(4.0) // 1.5 + 2.5
	
	if !bidVolume.Equal(expectedBidVolume) {
		t.Errorf("Expected bid volume 5.0, got %s", bidVolume)
	}
	
	if !askVolume.Equal(expectedAskVolume) {
		t.Errorf("Expected ask volume 4.0, got %s", askVolume)
	}
}

func TestClear(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add some orders
	for i := 0; i < 10; i++ {
		order := &models.Order{
			ID:             uuid.New(),
			ClientID:       "client",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideBuy,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(50000.0 + float64(i*10)),
			Quantity:       decimal.NewFromFloat(1.0),
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
		}
		ob.AddOrder(order)
	}
	
	if ob.Size() != 10 {
		t.Errorf("Expected 10 orders, got %d", ob.Size())
	}
	
	ob.Clear()
	
	if ob.Size() != 0 {
		t.Errorf("Expected empty order book after clear, got size %d", ob.Size())
	}
	
	if ob.GetBestBid() != nil {
		t.Error("Expected no best bid after clear")
	}
	
	if ob.GetBestAsk() != nil {
		t.Error("Expected no best ask after clear")
	}
}

func TestCancelOrder(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add an order
	order := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(order)
	
	// Verify order exists
	if ob.Size() != 1 {
		t.Errorf("Expected order book size 1, got %d", ob.Size())
	}
	
	// Cancel the order using UUID
	cancelled := ob.CancelOrder(order.ID)
	
	if cancelled == nil {
		t.Fatal("Failed to cancel order")
	}
	
	// Verify order status is cancelled
	if cancelled.Status != models.OrderStatusCancelled {
		t.Errorf("Expected status Cancelled, got %v", cancelled.Status)
	}
	
	// Verify order is removed from order book
	if ob.Size() != 0 {
		t.Errorf("Expected empty order book after cancellation, got size %d", ob.Size())
	}
	
	// Verify order cannot be retrieved
	retrieved := ob.GetOrder(order.ID.String())
	if retrieved != nil {
		t.Error("Cancelled order should not exist in order book")
	}
	
	// Verify price level is removed (since it was the only order)
	if ob.GetBestBid() != nil {
		t.Error("Price level should be removed when empty")
	}
}

func TestCancelOrderRemovesEmptyPriceLevel(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add two orders at the same price
	order1 := &models.Order{
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
	
	order2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(order1)
	ob.AddOrder(order2)
	
	// Verify both orders exist
	if ob.Size() != 2 {
		t.Errorf("Expected 2 orders, got %d", ob.Size())
	}
	
	// Verify price level exists with correct volume
	bestBid := ob.GetBestBid()
	if bestBid == nil {
		t.Fatal("Expected best bid to exist")
	}
	if !bestBid.Volume.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected volume 3.0, got %s", bestBid.Volume)
	}
	
	// Cancel first order
	ob.CancelOrder(order1.ID)
	
	// Price level should still exist
	bestBid = ob.GetBestBid()
	if bestBid == nil {
		t.Fatal("Expected best bid to still exist after cancelling one order")
	}
	
	// Cancel second order
	ob.CancelOrder(order2.ID)
	
	// Price level should now be removed
	bestBid = ob.GetBestBid()
	if bestBid != nil {
		t.Error("Price level should be removed when all orders cancelled")
	}
	
	if ob.Size() != 0 {
		t.Errorf("Expected empty order book, got size %d", ob.Size())
	}
}

func TestCancelOrderNonExistent(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Try to cancel an order that doesn't exist
	nonExistentID := uuid.New()
	cancelled := ob.CancelOrder(nonExistentID)
	
	if cancelled != nil {
		t.Error("Should return nil when cancelling non-existent order")
	}
}

func TestPeekBest(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add bid orders
	bid1 := &models.Order{
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
	
	bid2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50100.0), // Higher bid (best)
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	// Add ask orders
	ask1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(51000.0), // Lower ask (best)
		Quantity:       decimal.NewFromFloat(1.5),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ask2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client4",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(51100.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(bid1)
	ob.AddOrder(bid2)
	ob.AddOrder(ask1)
	ob.AddOrder(ask2)
	
	// Peek at best prices
	bestBid, bestAsk := ob.PeekBest()
	
	if bestBid == nil {
		t.Fatal("Expected best bid to exist")
	}
	if !bestBid.Price.Equal(decimal.NewFromFloat(50100.0)) {
		t.Errorf("Expected best bid 50100.0, got %s", bestBid.Price)
	}
	
	if bestAsk == nil {
		t.Fatal("Expected best ask to exist")
	}
	if !bestAsk.Price.Equal(decimal.NewFromFloat(51000.0)) {
		t.Errorf("Expected best ask 51000.0, got %s", bestAsk.Price)
	}
	
	// Verify orders are still in the book (peek doesn't remove)
	if ob.Size() != 4 {
		t.Errorf("Expected 4 orders after peek, got %d", ob.Size())
	}
}

func TestPeekBestEmptyBook(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	bestBid, bestAsk := ob.PeekBest()
	
	if bestBid != nil {
		t.Error("Expected nil best bid for empty book")
	}
	
	if bestAsk != nil {
		t.Error("Expected nil best ask for empty book")
	}
}

func TestPopBestLevelBids(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add multiple orders at the best bid price
	bid1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50100.0), // Best bid
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	bid2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50100.0), // Same best bid
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	bid3 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0), // Lower bid
		Quantity:       decimal.NewFromFloat(1.5),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(bid1)
	ob.AddOrder(bid2)
	ob.AddOrder(bid3)
	
	initialSize := ob.Size()
	if initialSize != 3 {
		t.Errorf("Expected 3 orders initially, got %d", initialSize)
	}
	
	// Pop the best bid level (should remove bid1 and bid2)
	poppedLevel := ob.PopBestLevel(true)
	
	if poppedLevel == nil {
		t.Fatal("Expected to pop a price level")
	}
	
	if !poppedLevel.Price.Equal(decimal.NewFromFloat(50100.0)) {
		t.Errorf("Expected popped price 50100.0, got %s", poppedLevel.Price)
	}
	
	if poppedLevel.Orders.Len() != 2 {
		t.Errorf("Expected 2 orders in popped level, got %d", poppedLevel.Orders.Len())
	}
	
	expectedVolume := decimal.NewFromFloat(3.0) // 1.0 + 2.0
	if !poppedLevel.Volume.Equal(expectedVolume) {
		t.Errorf("Expected volume 3.0, got %s", poppedLevel.Volume)
	}
	
	// Verify orders were removed from the book
	if ob.Size() != 1 {
		t.Errorf("Expected 1 order remaining, got %d", ob.Size())
	}
	
	// Verify the new best bid is the lower price
	newBestBid := ob.GetBestBid()
	if newBestBid == nil {
		t.Fatal("Expected a new best bid")
	}
	if !newBestBid.Price.Equal(decimal.NewFromFloat(50000.0)) {
		t.Errorf("Expected new best bid 50000.0, got %s", newBestBid.Price)
	}
	
	// Verify removed orders can't be retrieved
	if ob.GetOrder(bid1.ID.String()) != nil {
		t.Error("Order should be removed from hash map")
	}
	if ob.GetOrder(bid2.ID.String()) != nil {
		t.Error("Order should be removed from hash map")
	}
}

func TestPopBestLevelAsks(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add multiple orders at different ask prices
	ask1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(51000.0), // Best ask (lowest)
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ask2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(51000.0), // Same best ask
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ask3 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(51100.0), // Higher ask
		Quantity:       decimal.NewFromFloat(1.5),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddOrder(ask1)
	ob.AddOrder(ask2)
	ob.AddOrder(ask3)
	
	// Pop the best ask level (should remove ask1 and ask2)
	poppedLevel := ob.PopBestLevel(false)
	
	if poppedLevel == nil {
		t.Fatal("Expected to pop a price level")
	}
	
	if !poppedLevel.Price.Equal(decimal.NewFromFloat(51000.0)) {
		t.Errorf("Expected popped price 51000.0, got %s", poppedLevel.Price)
	}
	
	if poppedLevel.Orders.Len() != 2 {
		t.Errorf("Expected 2 orders in popped level, got %d", poppedLevel.Orders.Len())
	}
	
	// Verify orders were removed
	if ob.Size() != 1 {
		t.Errorf("Expected 1 order remaining, got %d", ob.Size())
	}
	
	// Verify the new best ask is the higher price
	newBestAsk := ob.GetBestAsk()
	if newBestAsk == nil {
		t.Fatal("Expected a new best ask")
	}
	if !newBestAsk.Price.Equal(decimal.NewFromFloat(51100.0)) {
		t.Errorf("Expected new best ask 51100.0, got %s", newBestAsk.Price)
	}
}

func TestPopBestLevelEmptyBook(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Try to pop from empty book
	poppedBid := ob.PopBestLevel(true)
	if poppedBid != nil {
		t.Error("Expected nil when popping from empty bids")
	}
	
	poppedAsk := ob.PopBestLevel(false)
	if poppedAsk != nil {
		t.Error("Expected nil when popping from empty asks")
	}
}

func TestPopBestLevelMaxHeapBehavior(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add bids in random order
	prices := []float64{50000.0, 50300.0, 50100.0, 50200.0}
	
	for _, price := range prices {
		order := &models.Order{
			ID:             uuid.New(),
			ClientID:       "client",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideBuy,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(price),
			Quantity:       decimal.NewFromFloat(1.0),
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
		}
		ob.AddOrder(order)
	}
	
	// Pop should return highest price first (max-heap behavior)
	level1 := ob.PopBestLevel(true)
	if !level1.Price.Equal(decimal.NewFromFloat(50300.0)) {
		t.Errorf("Expected first pop to be 50300.0, got %s", level1.Price)
	}
	
	level2 := ob.PopBestLevel(true)
	if !level2.Price.Equal(decimal.NewFromFloat(50200.0)) {
		t.Errorf("Expected second pop to be 50200.0, got %s", level2.Price)
	}
	
	level3 := ob.PopBestLevel(true)
	if !level3.Price.Equal(decimal.NewFromFloat(50100.0)) {
		t.Errorf("Expected third pop to be 50100.0, got %s", level3.Price)
	}
	
	level4 := ob.PopBestLevel(true)
	if !level4.Price.Equal(decimal.NewFromFloat(50000.0)) {
		t.Errorf("Expected fourth pop to be 50000.0, got %s", level4.Price)
	}
	
	// Book should be empty now
	if ob.Size() != 0 {
		t.Errorf("Expected empty book, got size %d", ob.Size())
	}
}

func TestPopBestLevelMinHeapBehavior(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add asks in random order
	prices := []float64{51300.0, 51000.0, 51200.0, 51100.0}
	
	for _, price := range prices {
		order := &models.Order{
			ID:             uuid.New(),
			ClientID:       "client",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideSell,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(price),
			Quantity:       decimal.NewFromFloat(1.0),
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
		}
		ob.AddOrder(order)
	}
	
	// Pop should return lowest price first (min-heap behavior)
	level1 := ob.PopBestLevel(false)
	if !level1.Price.Equal(decimal.NewFromFloat(51000.0)) {
		t.Errorf("Expected first pop to be 51000.0, got %s", level1.Price)
	}
	
	level2 := ob.PopBestLevel(false)
	if !level2.Price.Equal(decimal.NewFromFloat(51100.0)) {
		t.Errorf("Expected second pop to be 51100.0, got %s", level2.Price)
	}
	
	level3 := ob.PopBestLevel(false)
	if !level3.Price.Equal(decimal.NewFromFloat(51200.0)) {
		t.Errorf("Expected third pop to be 51200.0, got %s", level3.Price)
	}
	
	level4 := ob.PopBestLevel(false)
	if !level4.Price.Equal(decimal.NewFromFloat(51300.0)) {
		t.Errorf("Expected fourth pop to be 51300.0, got %s", level4.Price)
	}
	
	// Book should be empty now
	if ob.Size() != 0 {
		t.Errorf("Expected empty book, got size %d", ob.Size())
	}
}

func TestAddLimitOrderBasic(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	order := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	returnedOrder := ob.AddLimitOrder(order)
	
	if returnedOrder == nil {
		t.Fatal("AddLimitOrder should return the order")
	}
	
	if returnedOrder.ID != order.ID {
		t.Error("Returned order ID does not match")
	}
	
	if ob.Size() != 1 {
		t.Errorf("Expected 1 order in book, got %d", ob.Size())
	}
	
	// Verify order can be retrieved
	retrieved := ob.GetOrder(order.ID.String())
	if retrieved == nil {
		t.Fatal("Order should be retrievable from book")
	}
	
	if !retrieved.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected price 100.0, got %s", retrieved.Price)
	}
}

func TestAddLimitOrderRejectsMarketOrder(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	marketOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeMarket, // Market order, not limit
		Price:          decimal.Zero,
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	returnedOrder := ob.AddLimitOrder(marketOrder)
	
	if returnedOrder != nil {
		t.Error("AddLimitOrder should reject market orders (return nil)")
	}
	
	if ob.Size() != 0 {
		t.Error("Market order should not be added to book")
	}
}

func TestAddLimitOrderPriceTimePriority(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add three orders at the SAME price in sequence
	// They should be ordered by time (FIFO)
	
	order1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	order2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0), // Same price
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	order3 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0), // Same price
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	// Add in sequence
	ob.AddLimitOrder(order1)
	ob.AddLimitOrder(order2)
	ob.AddLimitOrder(order3)
	
	if ob.Size() != 3 {
		t.Fatalf("Expected 3 orders, got %d", ob.Size())
	}
	
	// Get the price level
	bestBid := ob.GetBestBid()
	if bestBid == nil {
		t.Fatal("Expected best bid to exist")
	}
	
	if !bestBid.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected price 100.0, got %s", bestBid.Price)
	}
	
	// Verify FIFO ordering: orders should be in the list in the order they were added
	if bestBid.Orders.Len() != 3 {
		t.Fatalf("Expected 3 orders at price level, got %d", bestBid.Orders.Len())
	}
	
	// Check order 1 is first (FIFO)
	element := bestBid.Orders.Front()
	firstOrder := element.Value.(*models.Order)
	if firstOrder.ID != order1.ID {
		t.Error("Expected order1 to be first (time priority)")
	}
	if !firstOrder.Quantity.Equal(decimal.NewFromFloat(1.0)) {
		t.Errorf("Expected first order quantity 1.0, got %s", firstOrder.Quantity)
	}
	
	// Check order 2 is second
	element = element.Next()
	secondOrder := element.Value.(*models.Order)
	if secondOrder.ID != order2.ID {
		t.Error("Expected order2 to be second (time priority)")
	}
	if !secondOrder.Quantity.Equal(decimal.NewFromFloat(2.0)) {
		t.Errorf("Expected second order quantity 2.0, got %s", secondOrder.Quantity)
	}
	
	// Check order 3 is third
	element = element.Next()
	thirdOrder := element.Value.(*models.Order)
	if thirdOrder.ID != order3.ID {
		t.Error("Expected order3 to be third (time priority)")
	}
	if !thirdOrder.Quantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected third order quantity 3.0, got %s", thirdOrder.Quantity)
	}
	
	// Verify there's no fourth element
	if element.Next() != nil {
		t.Error("Expected only 3 orders in the list")
	}
}

func TestAddLimitOrderMultiplePriceLevels(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add orders at different prices
	// Higher bids should be "better" (max-heap)
	
	order1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	order2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(102.0), // Higher price (better bid)
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	order3 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(101.0), // Middle price
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddLimitOrder(order1)
	ob.AddLimitOrder(order2)
	ob.AddLimitOrder(order3)
	
	// Best bid should be the highest price (102.0)
	bestBid := ob.GetBestBid()
	if !bestBid.Price.Equal(decimal.NewFromFloat(102.0)) {
		t.Errorf("Expected best bid 102.0, got %s", bestBid.Price)
	}
	
	// There should be 3 price levels
	if ob.Bids.Len() != 3 {
		t.Errorf("Expected 3 bid price levels, got %d", ob.Bids.Len())
	}
}

func TestAddLimitOrderSamePriceDifferentSides(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add buy and sell orders at crossing prices
	buyOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "buyer",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	sellOrder := &models.Order{
		ID:             uuid.New(),
		ClientID:       "seller",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0), // Same price
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	// Add both orders (AddLimitOrder doesn't match, just adds)
	ob.AddLimitOrder(buyOrder)
	ob.AddLimitOrder(sellOrder)
	
	if ob.Size() != 2 {
		t.Errorf("Expected 2 orders, got %d", ob.Size())
	}
	
	// Both should be in the book
	bestBid := ob.GetBestBid()
	bestAsk := ob.GetBestAsk()
	
	if bestBid == nil || bestAsk == nil {
		t.Fatal("Both best bid and best ask should exist")
	}
	
	if !bestBid.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected best bid 100.0, got %s", bestBid.Price)
	}
	
	if !bestAsk.Price.Equal(decimal.NewFromFloat(100.0)) {
		t.Errorf("Expected best ask 100.0, got %s", bestAsk.Price)
	}
	
	// Spread should be 0 (crossed book - would match in real matching engine)
	spread := ob.GetSpread()
	if !spread.Equal(decimal.Zero) {
		t.Errorf("Expected spread 0, got %s", spread)
	}
}

func TestAddLimitOrderVolumeCalculation(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add multiple orders at the same price
	totalQuantity := decimal.Zero
	
	for i := 0; i < 5; i++ {
		qty := decimal.NewFromFloat(float64(i + 1))
		totalQuantity = totalQuantity.Add(qty)
		
		order := &models.Order{
			ID:             uuid.New(),
			ClientID:       "client",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideBuy,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(100.0),
			Quantity:       qty,
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
		}
		
		ob.AddLimitOrder(order)
	}
	
	// Check total volume at price level
	bestBid := ob.GetBestBid()
	if bestBid == nil {
		t.Fatal("Expected best bid to exist")
	}
	
	expectedVolume := decimal.NewFromFloat(15.0) // 1+2+3+4+5 = 15
	if !bestBid.Volume.Equal(expectedVolume) {
		t.Errorf("Expected volume 15.0, got %s", bestBid.Volume)
	}
	
	if bestBid.Orders.Len() != 5 {
		t.Errorf("Expected 5 orders, got %d", bestBid.Orders.Len())
	}
}

func TestAddLimitOrderFIFOWithRemoval(t *testing.T) {
	ob := NewOrderBook("BTC-USD")
	
	// Add 3 orders at same price
	order1 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	order2 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client2",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(2.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	order3 := &models.Order{
		ID:             uuid.New(),
		ClientID:       "client3",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(100.0),
		Quantity:       decimal.NewFromFloat(3.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
	}
	
	ob.AddLimitOrder(order1)
	ob.AddLimitOrder(order2)
	ob.AddLimitOrder(order3)
	
	// Remove the middle order (order2)
	ob.RemoveOrder(order2.ID.String())
	
	if ob.Size() != 2 {
		t.Errorf("Expected 2 orders after removal, got %d", ob.Size())
	}
	
	// Check FIFO order is preserved (order1 then order3)
	bestBid := ob.GetBestBid()
	if bestBid.Orders.Len() != 2 {
		t.Fatalf("Expected 2 orders at price level, got %d", bestBid.Orders.Len())
	}
	
	element := bestBid.Orders.Front()
	firstOrder := element.Value.(*models.Order)
	if firstOrder.ID != order1.ID {
		t.Error("Expected order1 to still be first")
	}
	
	element = element.Next()
	secondOrder := element.Value.(*models.Order)
	if secondOrder.ID != order3.ID {
		t.Error("Expected order3 to be second (after order2 removed)")
	}
}
