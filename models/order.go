package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// OrderSide represents the side of an order (buy or sell)
type OrderSide string

const (
	OrderSideBuy  OrderSide = "buy"
	OrderSideSell OrderSide = "sell"
)

// OrderType represents the type of order (limit or market)
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

// OrderStatus represents the current status of an order
type OrderStatus string

const (
	OrderStatusOpen            OrderStatus = "open"
	OrderStatusPartiallyFilled OrderStatus = "partially_filled"
	OrderStatusFilled          OrderStatus = "filled"
	OrderStatusCancelled       OrderStatus = "cancelled"
	OrderStatusRejected        OrderStatus = "rejected"
)

// Order represents a trading order in the system
type Order struct {
	ID             uuid.UUID       `json:"id" db:"order_id"`
	ClientID       string          `json:"client_id" db:"client_id"`
	Instrument     string          `json:"instrument" db:"instrument"`
	Side           OrderSide       `json:"side" db:"side"`
	Type           OrderType       `json:"type" db:"type"`
	Price          decimal.Decimal `json:"price" db:"price"`
	Quantity       decimal.Decimal `json:"quantity" db:"quantity"`
	FilledQuantity decimal.Decimal `json:"filled_quantity" db:"filled_quantity"`
	Status         OrderStatus     `json:"status" db:"status"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" db:"updated_at"`
}

// NewOrder creates a new Order instance with default values
func NewOrder(clientID, instrument string, side OrderSide, orderType OrderType, price, quantity decimal.Decimal) *Order {
	now := time.Now()
	return &Order{
		ID:             uuid.New(),
		ClientID:       clientID,
		Instrument:     instrument,
		Side:           side,
		Type:           orderType,
		Price:          price,
		Quantity:       quantity,
		FilledQuantity: decimal.Zero,
		Status:         OrderStatusOpen,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// IsValid validates the order fields
func (o *Order) IsValid() bool {
	// Check required fields
	if o.ClientID == "" || o.Instrument == "" {
		return false
	}

	// Validate side
	if o.Side != OrderSideBuy && o.Side != OrderSideSell {
		return false
	}

	// Validate type
	if o.Type != OrderTypeLimit && o.Type != OrderTypeMarket {
		return false
	}

	// Validate quantity is positive
	if o.Quantity.LessThanOrEqual(decimal.Zero) {
		return false
	}

	// For limit orders, price must be positive
	if o.Type == OrderTypeLimit && o.Price.LessThanOrEqual(decimal.Zero) {
		return false
	}

	// FilledQuantity cannot exceed Quantity
	if o.FilledQuantity.GreaterThan(o.Quantity) {
		return false
	}

	return true
}

// RemainingQuantity returns the unfilled quantity of the order
func (o *Order) RemainingQuantity() decimal.Decimal {
	return o.Quantity.Sub(o.FilledQuantity)
}

// IsFilled checks if the order is completely filled
func (o *Order) IsFilled() bool {
	return o.FilledQuantity.Equal(o.Quantity)
}

// IsPartiallyFilled checks if the order is partially filled
func (o *Order) IsPartiallyFilled() bool {
	return o.FilledQuantity.GreaterThan(decimal.Zero) && o.FilledQuantity.LessThan(o.Quantity)
}

// CanBeFilled checks if the order can be filled (is open or partially filled)
func (o *Order) CanBeFilled() bool {
	return o.Status == OrderStatusOpen || o.Status == OrderStatusPartiallyFilled
}

// Fill updates the order with a fill amount
func (o *Order) Fill(quantity decimal.Decimal) {
	o.FilledQuantity = o.FilledQuantity.Add(quantity)
	o.UpdatedAt = time.Now()

	if o.IsFilled() {
		o.Status = OrderStatusFilled
	} else if o.IsPartiallyFilled() {
		o.Status = OrderStatusPartiallyFilled
	}
}

// Cancel marks the order as cancelled
func (o *Order) Cancel() {
	o.Status = OrderStatusCancelled
	o.UpdatedAt = time.Now()
}

// Reject marks the order as rejected
func (o *Order) Reject() {
	o.Status = OrderStatusRejected
	o.UpdatedAt = time.Now()
}
