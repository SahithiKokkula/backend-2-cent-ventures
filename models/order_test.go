package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNewOrder(t *testing.T) {
	clientID := "client-123"
	instrument := "BTC-USD"
	side := OrderSideBuy
	orderType := OrderTypeLimit
	price := decimal.NewFromFloat(70000.50)
	quantity := decimal.NewFromFloat(1.5)

	order := NewOrder(clientID, instrument, side, orderType, price, quantity)

	// Verify fields
	if order.ClientID != clientID {
		t.Errorf("Expected ClientID %s, got %s", clientID, order.ClientID)
	}
	if order.Instrument != instrument {
		t.Errorf("Expected Instrument %s, got %s", instrument, order.Instrument)
	}
	if order.Side != side {
		t.Errorf("Expected Side %s, got %s", side, order.Side)
	}
	if order.Type != orderType {
		t.Errorf("Expected Type %s, got %s", orderType, order.Type)
	}
	if !order.Price.Equal(price) {
		t.Errorf("Expected Price %s, got %s", price, order.Price)
	}
	if !order.Quantity.Equal(quantity) {
		t.Errorf("Expected Quantity %s, got %s", quantity, order.Quantity)
	}
	if !order.FilledQuantity.IsZero() {
		t.Errorf("Expected FilledQuantity to be zero, got %s", order.FilledQuantity)
	}
	if order.Status != OrderStatusOpen {
		t.Errorf("Expected Status %s, got %s", OrderStatusOpen, order.Status)
	}
	if order.ID == uuid.Nil {
		t.Error("Expected ID to be generated")
	}
}

func TestOrderIsValid(t *testing.T) {
	tests := []struct {
		name  string
		order *Order
		valid bool
	}{
		{
			name: "valid limit order",
			order: &Order{
				ClientID:   "client-1",
				Instrument: "BTC-USD",
				Side:       OrderSideBuy,
				Type:       OrderTypeLimit,
				Price:      decimal.NewFromFloat(70000),
				Quantity:   decimal.NewFromFloat(1.0),
			},
			valid: true,
		},
		{
			name: "valid market order",
			order: &Order{
				ClientID:   "client-1",
				Instrument: "BTC-USD",
				Side:       OrderSideSell,
				Type:       OrderTypeMarket,
				Quantity:   decimal.NewFromFloat(1.0),
			},
			valid: true,
		},
		{
			name: "invalid - empty client ID",
			order: &Order{
				ClientID:   "",
				Instrument: "BTC-USD",
				Side:       OrderSideBuy,
				Type:       OrderTypeLimit,
				Price:      decimal.NewFromFloat(70000),
				Quantity:   decimal.NewFromFloat(1.0),
			},
			valid: false,
		},
		{
			name: "invalid - zero quantity",
			order: &Order{
				ClientID:   "client-1",
				Instrument: "BTC-USD",
				Side:       OrderSideBuy,
				Type:       OrderTypeLimit,
				Price:      decimal.NewFromFloat(70000),
				Quantity:   decimal.Zero,
			},
			valid: false,
		},
		{
			name: "invalid - negative price for limit order",
			order: &Order{
				ClientID:   "client-1",
				Instrument: "BTC-USD",
				Side:       OrderSideBuy,
				Type:       OrderTypeLimit,
				Price:      decimal.NewFromFloat(-70000),
				Quantity:   decimal.NewFromFloat(1.0),
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.order.IsValid(); got != tt.valid {
				t.Errorf("IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestOrderRemainingQuantity(t *testing.T) {
	order := NewOrder("client-1", "BTC-USD", OrderSideBuy, OrderTypeLimit,
		decimal.NewFromFloat(70000), decimal.NewFromFloat(10.0))

	// Initially, remaining should equal quantity
	if !order.RemainingQuantity().Equal(decimal.NewFromFloat(10.0)) {
		t.Errorf("Expected remaining quantity 10.0, got %s", order.RemainingQuantity())
	}

	// After partial fill
	order.Fill(decimal.NewFromFloat(3.0))
	if !order.RemainingQuantity().Equal(decimal.NewFromFloat(7.0)) {
		t.Errorf("Expected remaining quantity 7.0, got %s", order.RemainingQuantity())
	}
}

func TestOrderFill(t *testing.T) {
	order := NewOrder("client-1", "BTC-USD", OrderSideBuy, OrderTypeLimit,
		decimal.NewFromFloat(70000), decimal.NewFromFloat(10.0))

	// Partial fill
	order.Fill(decimal.NewFromFloat(3.0))
	if order.Status != OrderStatusPartiallyFilled {
		t.Errorf("Expected status %s, got %s", OrderStatusPartiallyFilled, order.Status)
	}
	if !order.FilledQuantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected filled quantity 3.0, got %s", order.FilledQuantity)
	}

	// Complete fill
	order.Fill(decimal.NewFromFloat(7.0))
	if order.Status != OrderStatusFilled {
		t.Errorf("Expected status %s, got %s", OrderStatusFilled, order.Status)
	}
	if !order.IsFilled() {
		t.Error("Expected order to be filled")
	}
}

func TestOrderCanBeFilled(t *testing.T) {
	order := NewOrder("client-1", "BTC-USD", OrderSideBuy, OrderTypeLimit,
		decimal.NewFromFloat(70000), decimal.NewFromFloat(10.0))

	// Open order can be filled
	if !order.CanBeFilled() {
		t.Error("Expected open order to be fillable")
	}

	// Partially filled order can be filled
	order.Fill(decimal.NewFromFloat(5.0))
	if !order.CanBeFilled() {
		t.Error("Expected partially filled order to be fillable")
	}

	// Cancelled order cannot be filled
	order.Cancel()
	if order.CanBeFilled() {
		t.Error("Expected cancelled order not to be fillable")
	}
}

func TestOrderCancel(t *testing.T) {
	order := NewOrder("client-1", "BTC-USD", OrderSideBuy, OrderTypeLimit,
		decimal.NewFromFloat(70000), decimal.NewFromFloat(10.0))

	oldTime := order.UpdatedAt
	time.Sleep(10 * time.Millisecond)
	
	order.Cancel()
	
	if order.Status != OrderStatusCancelled {
		t.Errorf("Expected status %s, got %s", OrderStatusCancelled, order.Status)
	}
	if !order.UpdatedAt.After(oldTime) {
		t.Error("Expected UpdatedAt to be updated")
	}
}

func TestOrderReject(t *testing.T) {
	order := NewOrder("client-1", "BTC-USD", OrderSideBuy, OrderTypeLimit,
		decimal.NewFromFloat(70000), decimal.NewFromFloat(10.0))

	order.Reject()
	
	if order.Status != OrderStatusRejected {
		t.Errorf("Expected status %s, got %s", OrderStatusRejected, order.Status)
	}
}
