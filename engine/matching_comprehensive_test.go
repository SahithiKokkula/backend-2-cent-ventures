package engine

import (
	"context"
	"testing"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper functions for creating test orders
func newTestOrder(clientID string, side models.OrderSide, orderType models.OrderType, price, quantity string) *models.Order {
	priceDecimal, _ := decimal.NewFromString(price)
	quantityDecimal, _ := decimal.NewFromString(quantity)

	return &models.Order{
		ID:             uuid.New(),
		ClientID:       clientID,
		Instrument:     "BTC-USD",
		Side:           side,
		Type:           orderType,
		Price:          priceDecimal,
		Quantity:       quantityDecimal,
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
	}
}

func newLimitOrder(clientID string, side models.OrderSide, price, quantity string) *models.Order {
	return newTestOrder(clientID, side, models.OrderTypeLimit, price, quantity)
}

func newMarketOrder(clientID string, side models.OrderSide, quantity string) *models.Order {
	return newTestOrder(clientID, side, models.OrderTypeMarket, "0", quantity)
}

// TestMatchingEngine_ComprehensiveSuite runs comprehensive table-driven tests
func TestMatchingEngine_ComprehensiveSuite(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*MatchingEngine)
		incomingOrder  *models.Order
		expectedTrades int
		validate       func(*testing.T, *MatchingEngine, []*Trade, *models.Order)
	}{
		{
			name: "Limit order added to empty book",
			setup: func(me *MatchingEngine) {
				// No setup - empty book
			},
			incomingOrder:  newLimitOrder("client1", models.OrderSideBuy, "50000", "1.0"),
			expectedTrades: 0,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				assert.Equal(t, 0, len(trades), "No trades should occur on empty book")
				assert.Equal(t, models.OrderStatusOpen, order.Status, "Order should be open")

				expectedQty := decimal.RequireFromString("1.0")
				assert.True(t, order.RemainingQuantity().Equal(expectedQty), "Full quantity should remain")

				// Verify order is in the book
				bids, _ := me.orderBook.GetTopLevels(10)
				require.Equal(t, 1, len(bids), "Should have 1 bid level")
				assert.Equal(t, "50000", bids[0].Price.String())
				assert.True(t, bids[0].Volume.Equal(expectedQty), "Volume should be 1.0")
			},
		},
		{
			name: "Buy order fully filled by single sell order",
			setup: func(me *MatchingEngine) {
				// Add a sell order at 50000
				sellOrder := newLimitOrder("seller1", models.OrderSideSell, "50000", "1.0")
				_, _ = me.SubmitOrder(sellOrder)
			},
			incomingOrder:  newLimitOrder("buyer1", models.OrderSideBuy, "50000", "1.0"),
			expectedTrades: 1,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				require.Equal(t, 1, len(trades), "Should have 1 trade")

				// Validate trade
				trade := trades[0]
				assert.Equal(t, "50000", trade.Price.String(), "Trade price should be 50000")

				expectedQty := decimal.RequireFromString("1.0")
				assert.True(t, trade.Quantity.Equal(expectedQty), "Trade quantity should be 1.0")
				assert.Equal(t, order.ID, trade.BuyOrderID, "Buy order ID should match")

				// Validate order status
				assert.Equal(t, models.OrderStatusFilled, order.Status, "Order should be filled")
				assert.True(t, order.FilledQuantity.Equal(expectedQty), "Full quantity should be filled")

				expectedZero := decimal.Zero
				assert.True(t, order.RemainingQuantity().Equal(expectedZero), "No quantity should remain")

				// Verify book is empty
				bids, asks := me.orderBook.GetTopLevels(10)
				assert.Equal(t, 0, len(asks), "Ask side should be empty")
				assert.Equal(t, 0, len(bids), "Bid side should be empty")
			},
		},
		{
			name: "Buy order partially filled",
			setup: func(me *MatchingEngine) {
				// Add a sell order for 0.5 BTC at 50000
				sellOrder := newLimitOrder("seller1", models.OrderSideSell, "50000", "0.5")
				_, _ = me.SubmitOrder(sellOrder)
			},
			incomingOrder:  newLimitOrder("buyer1", models.OrderSideBuy, "50000", "1.0"),
			expectedTrades: 1,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				require.Equal(t, 1, len(trades), "Should have 1 trade")

				// Validate trade
				trade := trades[0]
				assert.Equal(t, "50000", trade.Price.String(), "Trade price should be 50000")
				assert.Equal(t, "0.5", trade.Quantity.String(), "Trade quantity should be 0.5")

				// Validate order status
				assert.Equal(t, models.OrderStatusPartiallyFilled, order.Status, "Order should be partially filled")
				assert.Equal(t, "0.5", order.FilledQuantity.String(), "0.5 should be filled")
				assert.Equal(t, "0.5", order.RemainingQuantity().String(), "0.5 should remain")

				// Verify remaining order is in book
				bids, _ := me.orderBook.GetTopLevels(10)
				require.Equal(t, 1, len(bids), "Should have 1 bid level")
				assert.Equal(t, "50000", bids[0].Price.String())
				assert.Equal(t, "0.5", bids[0].Volume.String(), "Remaining 0.5 should be in book")
			},
		},
		{
			name: "Buy order filled by multiple sell orders at different prices",
			setup: func(me *MatchingEngine) {
				// Add sell orders at different prices
				sell1 := newLimitOrder("seller1", models.OrderSideSell, "49000", "0.3")
				sell2 := newLimitOrder("seller2", models.OrderSideSell, "49500", "0.4")
				sell3 := newLimitOrder("seller3", models.OrderSideSell, "50000", "0.5")
				_, _ = me.SubmitOrder(sell1)
				_, _ = me.SubmitOrder(sell2)
				_, _ = me.SubmitOrder(sell3)
			},
			incomingOrder:  newLimitOrder("buyer1", models.OrderSideBuy, "50000", "1.0"),
			expectedTrades: 3,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				require.Equal(t, 3, len(trades), "Should have 3 trades")

				// Validate trades are in price-time priority (best price first)
				assert.Equal(t, "49000", trades[0].Price.String(), "First trade at best price 49000")
				assert.Equal(t, "0.3", trades[0].Quantity.String())

				assert.Equal(t, "49500", trades[1].Price.String(), "Second trade at 49500")
				assert.Equal(t, "0.4", trades[1].Quantity.String())

				assert.Equal(t, "50000", trades[2].Price.String(), "Third trade at 50000")
				assert.Equal(t, "0.3", trades[2].Quantity.String(), "Only 0.3 needed to complete 1.0")

				// Validate order is fully filled
				assert.Equal(t, models.OrderStatusFilled, order.Status, "Order should be fully filled")

				expectedFilled := decimal.RequireFromString("1.0")
				assert.True(t, order.FilledQuantity.Equal(expectedFilled), "1.0 should be filled")

				expectedZero := decimal.Zero
				assert.True(t, order.RemainingQuantity().Equal(expectedZero), "No quantity should remain")

				// Verify book has remaining 0.2 at 50000
				_, asks := me.orderBook.GetTopLevels(10)
				require.Equal(t, 1, len(asks), "Should have 1 ask level remaining")
				assert.Equal(t, "50000", asks[0].Price.String())
				assert.Equal(t, "0.2", asks[0].Volume.String(), "0.2 should remain at 50000")
			},
		},
		{
			name: "Market order filled correctly",
			setup: func(me *MatchingEngine) {
				// Add sell orders
				sell1 := newLimitOrder("seller1", models.OrderSideSell, "50000", "0.5")
				sell2 := newLimitOrder("seller2", models.OrderSideSell, "50500", "0.7")
				_, _ = me.SubmitOrder(sell1)
				_, _ = me.SubmitOrder(sell2)
			},
			incomingOrder:  newMarketOrder("buyer1", models.OrderSideBuy, "1.0"),
			expectedTrades: 2,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				require.Equal(t, 2, len(trades), "Should have 2 trades")

				// Market order takes best available prices
				assert.Equal(t, "50000", trades[0].Price.String(), "First at 50000")
				assert.Equal(t, "0.5", trades[0].Quantity.String())

				assert.Equal(t, "50500", trades[1].Price.String(), "Second at 50500")
				assert.Equal(t, "0.5", trades[1].Quantity.String())

				// Order fully filled
				assert.Equal(t, models.OrderStatusFilled, order.Status)

				expectedFilled := decimal.RequireFromString("1.0")
				assert.True(t, order.FilledQuantity.Equal(expectedFilled), "1.0 should be filled")

				// Book has remaining 0.2 at 50500
				_, asks := me.orderBook.GetTopLevels(10)
				require.Equal(t, 1, len(asks))
				assert.Equal(t, "0.2", asks[0].Volume.String())
			},
		},
		{
			name: "Same price FIFO ordering",
			setup: func(me *MatchingEngine) {
				// Add 3 sell orders at same price in specific order
				sell1 := newLimitOrder("seller1", models.OrderSideSell, "50000", "0.3")
				sell2 := newLimitOrder("seller2", models.OrderSideSell, "50000", "0.4")
				sell3 := newLimitOrder("seller3", models.OrderSideSell, "50000", "0.5")
				_, _ = me.SubmitOrder(sell1)
				time.Sleep(1 * time.Millisecond) // Ensure different timestamps
				_, _ = me.SubmitOrder(sell2)
				time.Sleep(1 * time.Millisecond)
				_, _ = me.SubmitOrder(sell3)
			},
			incomingOrder:  newLimitOrder("buyer1", models.OrderSideBuy, "50000", "1.0"),
			expectedTrades: 3,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				require.Equal(t, 3, len(trades), "Should have 3 trades")

				// All trades at same price
				for _, trade := range trades {
					assert.Equal(t, "50000", trade.Price.String(), "All trades at 50000")
				}

				// FIFO order: first order (0.3) filled first, then second (0.4), then partial third (0.3)
				assert.Equal(t, "0.3", trades[0].Quantity.String(), "First FIFO order filled")
				assert.Equal(t, "0.4", trades[1].Quantity.String(), "Second FIFO order filled")
				assert.Equal(t, "0.3", trades[2].Quantity.String(), "Third FIFO order partially filled")

				// Verify remaining 0.2 from third order
				_, asks := me.orderBook.GetTopLevels(10)
				require.Equal(t, 1, len(asks))
				assert.Equal(t, "0.2", asks[0].Volume.String(), "Remaining from third order")
			},
		},
		{
			name: "Empty price level removal",
			setup: func(me *MatchingEngine) {
				// Add sell order at 50000
				sell1 := newLimitOrder("seller1", models.OrderSideSell, "50000", "1.0")
				_, _ = me.SubmitOrder(sell1)
			},
			incomingOrder:  newLimitOrder("buyer1", models.OrderSideBuy, "50000", "1.0"),
			expectedTrades: 1,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				// Verify price level is completely removed
				bids, asks := me.orderBook.GetTopLevels(10)
				assert.Equal(t, 0, len(asks), "Price level should be removed when empty")
				assert.Equal(t, 0, len(bids), "Both sides should be empty")
			},
		},
		{
			name: "Market order exhausts entire book",
			setup: func(me *MatchingEngine) {
				// Add multiple sell orders
				sell1 := newLimitOrder("seller1", models.OrderSideSell, "50000", "0.5")
				sell2 := newLimitOrder("seller2", models.OrderSideSell, "51000", "0.3")
				_, _ = me.SubmitOrder(sell1)
				_, _ = me.SubmitOrder(sell2)
			},
			incomingOrder:  newMarketOrder("buyer1", models.OrderSideBuy, "2.0"),
			expectedTrades: 2,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				require.Equal(t, 2, len(trades), "Should execute all available orders")

				// Total filled is only 0.8 (0.5 + 0.3), not the requested 2.0
				totalFilled := decimal.Zero
				for _, trade := range trades {
					totalFilled = totalFilled.Add(trade.Quantity)
				}
				assert.Equal(t, "0.8", totalFilled.String(), "Only 0.8 available in book")

				// Order partially filled (can't fill 2.0, only 0.8 available)
				assert.Equal(t, models.OrderStatusPartiallyFilled, order.Status, "Partially filled due to insufficient liquidity")
				assert.Equal(t, "0.8", order.FilledQuantity.String())
				assert.Equal(t, "1.2", order.RemainingQuantity().String())

				// Book completely empty
				_, asks := me.orderBook.GetTopLevels(10)
				assert.Equal(t, 0, len(asks), "Book should be exhausted")
			},
		},
		{
			name: "Price-time priority with multiple levels",
			setup: func(me *MatchingEngine) {
				// Add multiple sell orders at different prices and times
				sell1 := newLimitOrder("seller1", models.OrderSideSell, "50000", "0.2")
				sell2 := newLimitOrder("seller2", models.OrderSideSell, "50000", "0.3")
				sell3 := newLimitOrder("seller3", models.OrderSideSell, "50500", "0.4")
				sell4 := newLimitOrder("seller4", models.OrderSideSell, "51000", "0.5")

				_, _ = me.SubmitOrder(sell1)
				time.Sleep(1 * time.Millisecond)
				_, _ = me.SubmitOrder(sell2)
				_, _ = me.SubmitOrder(sell3)
				_, _ = me.SubmitOrder(sell4)
			},
			incomingOrder:  newLimitOrder("buyer1", models.OrderSideBuy, "51000", "1.0"),
			expectedTrades: 4,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				require.Equal(t, 4, len(trades), "Should have 4 trades")

				// Validate price-time priority
				assert.Equal(t, "50000", trades[0].Price.String(), "Best price first")
				assert.Equal(t, "0.2", trades[0].Quantity.String(), "First order at 50000")

				assert.Equal(t, "50000", trades[1].Price.String())
				assert.Equal(t, "0.3", trades[1].Quantity.String(), "Second order at 50000 (FIFO)")

				assert.Equal(t, "50500", trades[2].Price.String(), "Second best price")
				assert.Equal(t, "0.4", trades[2].Quantity.String())

				assert.Equal(t, "51000", trades[3].Price.String(), "Third price")
				assert.Equal(t, "0.1", trades[3].Quantity.String(), "Partial fill to reach 1.0 total")

				// Order fully filled
				assert.Equal(t, models.OrderStatusFilled, order.Status)

				expectedFilled := decimal.RequireFromString("1.0")
				assert.True(t, order.FilledQuantity.Equal(expectedFilled), "1.0 should be filled")
			},
		},
		{
			name: "Sell order matches against multiple buy orders",
			setup: func(me *MatchingEngine) {
				// Add buy orders at different prices
				buy1 := newLimitOrder("buyer1", models.OrderSideBuy, "51000", "0.3")
				buy2 := newLimitOrder("buyer2", models.OrderSideBuy, "50500", "0.4")
				buy3 := newLimitOrder("buyer3", models.OrderSideBuy, "50000", "0.5")
				_, _ = me.SubmitOrder(buy1)
				_, _ = me.SubmitOrder(buy2)
				_, _ = me.SubmitOrder(buy3)
			},
			incomingOrder:  newLimitOrder("seller1", models.OrderSideSell, "50000", "1.0"),
			expectedTrades: 3,
			validate: func(t *testing.T, me *MatchingEngine, trades []*Trade, order *models.Order) {
				require.Equal(t, 3, len(trades), "Should have 3 trades")

				// Validate highest bid filled first
				assert.Equal(t, "51000", trades[0].Price.String(), "Highest bid first")
				assert.Equal(t, "0.3", trades[0].Quantity.String())

				assert.Equal(t, "50500", trades[1].Price.String(), "Second highest bid")
				assert.Equal(t, "0.4", trades[1].Quantity.String())

				assert.Equal(t, "50000", trades[2].Price.String(), "Third highest bid")
				assert.Equal(t, "0.3", trades[2].Quantity.String(), "Partial fill to complete 1.0")

				// Order fully filled
				assert.Equal(t, models.OrderStatusFilled, order.Status)

				// Verify remaining 0.2 at 50000
				bids, _ := me.orderBook.GetTopLevels(10)
				require.Equal(t, 1, len(bids))
				assert.Equal(t, "50000", bids[0].Price.String())
				assert.Equal(t, "0.2", bids[0].Volume.String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh matching engine for each test
			me := NewMatchingEngine("BTC-USD")
			_ = me.Start(context.Background())
			defer func() { _ = me.Stop() }() // Setup initial state
			tt.setup(me)

			// Submit the incoming order
			response, err := me.SubmitOrder(tt.incomingOrder)

			// Validate
			require.NoError(t, err, "SubmitOrder should not return error")
			require.NotNil(t, response, "Response should not be nil")
			assert.Equal(t, tt.expectedTrades, len(response.Trades), "Trade count mismatch")
			tt.validate(t, me, response.Trades, response.Order)
		})
	}
}
