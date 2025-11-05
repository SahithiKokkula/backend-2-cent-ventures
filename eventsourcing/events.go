// Package eventsourcing provides event sourcing primitives for the trading engine
package eventsourcing

import (
	"time"

	"github.com/google/uuid"
)

// ============================================
// Event Types
// ============================================

// BaseEvent contains common fields for all events
type BaseEvent struct {
	EventID       uuid.UUID              `json:"event_id"`
	EventType     string                 `json:"event_type"`
	AggregateID   string                 `json:"aggregate_id"`
	AggregateType string                 `json:"aggregate_type"`
	Version       int                    `json:"version"`
	Timestamp     time.Time              `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	CausationID   *uuid.UUID             `json:"causation_id,omitempty"`
	CorrelationID *uuid.UUID             `json:"correlation_id,omitempty"`
}

// OrderPlacedEvent represents a new order submission
type OrderPlacedEvent struct {
	BaseEvent
	OrderID     uuid.UUID `json:"order_id"`
	ClientID    string    `json:"client_id"`
	Instrument  string    `json:"instrument"`
	Side        string    `json:"side"` // "buy" or "sell"
	Type        string    `json:"type"` // "limit" or "market"
	Price       float64   `json:"price"`
	Quantity    float64   `json:"quantity"`
	TimeInForce string    `json:"time_in_force"` // "GTC", "IOC", "FOK"
}

// OrderCancelledEvent represents order cancellation
type OrderCancelledEvent struct {
	BaseEvent
	OrderID uuid.UUID `json:"order_id"`
	Reason  string    `json:"reason"` // "user_requested", "expired", "rejected"
}

// OrderPartiallyFilledEvent represents partial order fill
type OrderPartiallyFilledEvent struct {
	BaseEvent
	OrderID           uuid.UUID `json:"order_id"`
	FilledQuantity    float64   `json:"filled_quantity"`
	RemainingQuantity float64   `json:"remaining_quantity"`
	FillPrice         float64   `json:"fill_price"`
	TradeID           uuid.UUID `json:"trade_id"`
}

// OrderFilledEvent represents complete order fill
type OrderFilledEvent struct {
	BaseEvent
	OrderID      uuid.UUID `json:"order_id"`
	TotalFilled  float64   `json:"total_filled"`
	AveragePrice float64   `json:"average_price"`
	LastTradeID  uuid.UUID `json:"last_trade_id"`
}

// TradeExecutedEvent represents a completed trade
type TradeExecutedEvent struct {
	BaseEvent
	TradeID        uuid.UUID `json:"trade_id"`
	Instrument     string    `json:"instrument"`
	BuyerOrderID   uuid.UUID `json:"buyer_order_id"`
	SellerOrderID  uuid.UUID `json:"seller_order_id"`
	Price          float64   `json:"price"`
	Quantity       float64   `json:"quantity"`
	BuyerClientID  string    `json:"buyer_client_id"`
	SellerClientID string    `json:"seller_client_id"`
}

// ============================================
// Event Builder Functions
// ============================================

// NewOrderPlacedEvent creates a new OrderPlaced event
func NewOrderPlacedEvent(orderID uuid.UUID, clientID, instrument, side, orderType string,
	price, qty float64, tif string, version int) *OrderPlacedEvent {
	return &OrderPlacedEvent{
		BaseEvent: BaseEvent{
			EventID:       uuid.New(),
			EventType:     "OrderPlaced",
			AggregateID:   orderID.String(),
			AggregateType: "Order",
			Version:       version,
			Timestamp:     time.Now(),
			Metadata:      make(map[string]interface{}),
		},
		OrderID:     orderID,
		ClientID:    clientID,
		Instrument:  instrument,
		Side:        side,
		Type:        orderType,
		Price:       price,
		Quantity:    qty,
		TimeInForce: tif,
	}
}

// NewOrderCancelledEvent creates a new OrderCancelled event
func NewOrderCancelledEvent(orderID uuid.UUID, reason string, version int) *OrderCancelledEvent {
	return &OrderCancelledEvent{
		BaseEvent: BaseEvent{
			EventID:       uuid.New(),
			EventType:     "OrderCancelled",
			AggregateID:   orderID.String(),
			AggregateType: "Order",
			Version:       version,
			Timestamp:     time.Now(),
			Metadata:      make(map[string]interface{}),
		},
		OrderID: orderID,
		Reason:  reason,
	}
}

// NewTradeExecutedEvent creates a new TradeExecuted event
func NewTradeExecutedEvent(tradeID uuid.UUID, instrument string,
	buyerOrderID, sellerOrderID uuid.UUID, buyerClientID, sellerClientID string,
	price, qty float64, version int) *TradeExecutedEvent {
	return &TradeExecutedEvent{
		BaseEvent: BaseEvent{
			EventID:       uuid.New(),
			EventType:     "TradeExecuted",
			AggregateID:   instrument,
			AggregateType: "Orderbook",
			Version:       version,
			Timestamp:     time.Now(),
			Metadata:      make(map[string]interface{}),
		},
		TradeID:        tradeID,
		Instrument:     instrument,
		BuyerOrderID:   buyerOrderID,
		SellerOrderID:  sellerOrderID,
		Price:          price,
		Quantity:       qty,
		BuyerClientID:  buyerClientID,
		SellerClientID: sellerClientID,
	}
}

// ============================================
// Event Interface
// ============================================

// Event is the interface that all events must implement
type Event interface {
	GetEventID() uuid.UUID
	GetEventType() string
	GetAggregateID() string
	GetAggregateType() string
	GetVersion() int
	GetTimestamp() time.Time
}

// GetEventID returns the event ID
func (e *BaseEvent) GetEventID() uuid.UUID {
	return e.EventID
}

// GetEventType returns the event type
func (e *BaseEvent) GetEventType() string {
	return e.EventType
}

// GetAggregateID returns the aggregate ID
func (e *BaseEvent) GetAggregateID() string {
	return e.AggregateID
}

// GetAggregateType returns the aggregate type
func (e *BaseEvent) GetAggregateType() string {
	return e.AggregateType
}

// GetVersion returns the event version
func (e *BaseEvent) GetVersion() int {
	return e.Version
}

// GetTimestamp returns when the event occurred
func (e *BaseEvent) GetTimestamp() time.Time {
	return e.Timestamp
}

// ============================================
// Helper Functions
// ============================================

// WithMetadata adds metadata to an event
func WithMetadata(event Event, key string, value interface{}) {
	if be, ok := event.(*OrderPlacedEvent); ok {
		be.Metadata[key] = value
	} else if be, ok := event.(*OrderCancelledEvent); ok {
		be.Metadata[key] = value
	} else if be, ok := event.(*TradeExecutedEvent); ok {
		be.Metadata[key] = value
	}
}

// WithCorrelationID sets the correlation ID for event tracing
func WithCorrelationID(event Event, correlationID uuid.UUID) {
	if be, ok := event.(*OrderPlacedEvent); ok {
		be.CorrelationID = &correlationID
	} else if be, ok := event.(*OrderCancelledEvent); ok {
		be.CorrelationID = &correlationID
	} else if be, ok := event.(*TradeExecutedEvent); ok {
		be.CorrelationID = &correlationID
	}
}

// WithCausationID sets the causation ID (command that caused this event)
func WithCausationID(event Event, causationID uuid.UUID) {
	if be, ok := event.(*OrderPlacedEvent); ok {
		be.CausationID = &causationID
	} else if be, ok := event.(*OrderCancelledEvent); ok {
		be.CausationID = &causationID
	} else if be, ok := event.(*TradeExecutedEvent); ok {
		be.CausationID = &causationID
	}
}
