package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yourusername/trading-engine/models"
)

// OrderEventType represents the type of order event
type OrderEventType string

const (
	OrderEventCreated          OrderEventType = "ORDER_CREATED"
	OrderEventFilled           OrderEventType = "ORDER_FILLED"
	OrderEventPartiallyFilled  OrderEventType = "ORDER_PARTIALLY_FILLED"
	OrderEventCancelled        OrderEventType = "ORDER_CANCELLED"
	OrderEventRejected         OrderEventType = "ORDER_REJECTED"
	OrderEventUpdated          OrderEventType = "ORDER_UPDATED"
)

// OrderEvent represents an immutable event in the order lifecycle
type OrderEvent struct {
	EventID        int64
	OrderID        uuid.UUID
	EventType      OrderEventType
	EventData      map[string]interface{}
	EventTimestamp time.Time
	SequenceNumber int64
	TriggeredBy    string
	CreatedAt      time.Time
}

// OrderEventData structures for different event types
type OrderCreatedData struct {
	ClientID   string          `json:"client_id"`
	Instrument string          `json:"instrument"`
	Side       string          `json:"side"`
	Type       string          `json:"type"`
	Price      string          `json:"price"`
	Quantity   string          `json:"quantity"`
	Status     string          `json:"status"`
}

type OrderFillData struct {
	FillQuantity   string     `json:"fill_quantity"`
	FilledQuantity string     `json:"filled_quantity"`
	Status         string     `json:"status"`
	TradeID        *uuid.UUID `json:"trade_id,omitempty"`
}

type OrderCancelledData struct {
	Status string  `json:"status"`
	Reason *string `json:"reason,omitempty"`
}

type OrderRejectedData struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// OrderEventStore handles order event persistence
type OrderEventStore struct {
	db *sql.DB
}

// NewOrderEventStore creates a new order event store
func NewOrderEventStore(db *sql.DB) *OrderEventStore {
	return &OrderEventStore{db: db}
}

// RecordOrderCreated records an ORDER_CREATED event
func (oes *OrderEventStore) RecordOrderCreated(ctx context.Context, order *models.Order) (int64, error) {
	query := `SELECT record_order_created($1, $2, $3, $4, $5, $6, $7, $8)`

	var eventID int64
	err := oes.db.QueryRowContext(
		ctx,
		query,
		order.ID,
		order.ClientID,
		order.Instrument,
		string(order.Side),
		string(order.Type),
		order.Price.String(),
		order.Quantity.String(),
		order.CreatedAt,
	).Scan(&eventID)

	if err != nil {
		return 0, fmt.Errorf("failed to record order created event: %w", err)
	}

	return eventID, nil
}

// RecordOrderFill records an ORDER_FILLED or ORDER_PARTIALLY_FILLED event
func (oes *OrderEventStore) RecordOrderFill(
	ctx context.Context,
	orderID uuid.UUID,
	fillQuantity decimal.Decimal,
	newFilledQuantity decimal.Decimal,
	newStatus models.OrderStatus,
	tradeID *uuid.UUID,
	timestamp time.Time,
) (int64, error) {
	query := `SELECT record_order_fill($1, $2, $3, $4, $5, $6)`

	var eventID int64
	err := oes.db.QueryRowContext(
		ctx,
		query,
		orderID,
		fillQuantity.String(),
		newFilledQuantity.String(),
		string(newStatus),
		tradeID,
		timestamp,
	).Scan(&eventID)

	if err != nil {
		return 0, fmt.Errorf("failed to record order fill event: %w", err)
	}

	return eventID, nil
}

// RecordOrderCancelled records an ORDER_CANCELLED event
func (oes *OrderEventStore) RecordOrderCancelled(
	ctx context.Context,
	orderID uuid.UUID,
	reason *string,
	timestamp time.Time,
) (int64, error) {
	query := `SELECT record_order_cancelled($1, $2, $3)`

	var eventID int64
	err := oes.db.QueryRowContext(
		ctx,
		query,
		orderID,
		reason,
		timestamp,
	).Scan(&eventID)

	if err != nil {
		return 0, fmt.Errorf("failed to record order cancelled event: %w", err)
	}

	return eventID, nil
}

// RecordOrderRejected records an ORDER_REJECTED event
func (oes *OrderEventStore) RecordOrderRejected(
	ctx context.Context,
	orderID uuid.UUID,
	reason string,
	timestamp time.Time,
) (int64, error) {
	query := `SELECT record_order_rejected($1, $2, $3)`

	var eventID int64
	err := oes.db.QueryRowContext(
		ctx,
		query,
		orderID,
		reason,
		timestamp,
	).Scan(&eventID)

	if err != nil {
		return 0, fmt.Errorf("failed to record order rejected event: %w", err)
	}

	return eventID, nil
}

// GetOrderHistory retrieves all events for an order
func (oes *OrderEventStore) GetOrderHistory(ctx context.Context, orderID uuid.UUID) ([]*OrderEvent, error) {
	query := `SELECT * FROM get_order_history($1)`

	rows, err := oes.db.QueryContext(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get order history: %w", err)
	}
	defer rows.Close()

	var events []*OrderEvent
	for rows.Next() {
		var event OrderEvent
		var eventDataJSON []byte

		err := rows.Scan(
			&event.EventID,
			&event.EventType,
			&eventDataJSON,
			&event.EventTimestamp,
			&event.SequenceNumber,
			&event.TriggeredBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Parse JSON event data
		err = json.Unmarshal(eventDataJSON, &event.EventData)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
		}

		events = append(events, &event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating events: %w", err)
	}

	return events, nil
}

// ReplayOrderState replays all events to rebuild order state
func (oes *OrderEventStore) ReplayOrderState(ctx context.Context, orderID uuid.UUID) (*models.Order, error) {
	query := `SELECT * FROM replay_order_state($1)`

	var order models.Order
	var priceStr, quantityStr, filledQtyStr string
	var sideStr, typeStr, statusStr string
	var eventCount int64

	err := oes.db.QueryRowContext(ctx, query, orderID).Scan(
		&order.ID,
		&order.ClientID,
		&order.Instrument,
		&sideStr,
		&typeStr,
		&priceStr,
		&quantityStr,
		&filledQtyStr,
		&statusStr,
		&order.CreatedAt,
		&order.UpdatedAt,
		&eventCount,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to replay order state: %w", err)
	}

	// Parse decimal fields
	order.Price, err = decimal.NewFromString(priceStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse price: %w", err)
	}

	order.Quantity, err = decimal.NewFromString(quantityStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse quantity: %w", err)
	}

	order.FilledQuantity, err = decimal.NewFromString(filledQtyStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse filled quantity: %w", err)
	}

	// Parse enum fields
	order.Side = models.OrderSide(sideStr)
	order.Type = models.OrderType(typeStr)
	order.Status = models.OrderStatus(statusStr)

	return &order, nil
}

// RebuildSnapshot rebuilds the entire orders_snapshot table from events
func (oes *OrderEventStore) RebuildSnapshot(ctx context.Context) (int64, error) {
	query := `SELECT rebuild_orders_snapshot()`

	var count int64
	err := oes.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to rebuild snapshot: %w", err)
	}

	return count, nil
}

// VerifySnapshotConsistency checks if snapshot matches replayed state
func (oes *OrderEventStore) VerifySnapshotConsistency(ctx context.Context) ([]InconsistentOrder, error) {
	query := `SELECT * FROM verify_snapshot_consistency()`

	rows, err := oes.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to verify consistency: %w", err)
	}
	defer rows.Close()

	var inconsistencies []InconsistentOrder
	for rows.Next() {
		var inc InconsistentOrder
		var snapshotFilledStr, replayedFilledStr string

		err := rows.Scan(
			&inc.OrderID,
			&inc.SnapshotStatus,
			&inc.ReplayedStatus,
			&snapshotFilledStr,
			&replayedFilledStr,
			&inc.IsConsistent,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan inconsistency: %w", err)
		}

		inc.SnapshotFilled, _ = decimal.NewFromString(snapshotFilledStr)
		inc.ReplayedFilled, _ = decimal.NewFromString(replayedFilledStr)

		inconsistencies = append(inconsistencies, inc)
	}

	return inconsistencies, nil
}

// GetOrderFromSnapshot retrieves current order state from snapshot
func (oes *OrderEventStore) GetOrderFromSnapshot(ctx context.Context, orderID uuid.UUID) (*models.Order, error) {
	query := `
		SELECT order_id, client_id, instrument, side, type,
		       price, quantity, filled_quantity, status,
		       created_at, updated_at
		FROM orders_snapshot
		WHERE order_id = $1
	`

	var order models.Order
	var priceStr, quantityStr, filledQtyStr string
	var sideStr, typeStr, statusStr string

	err := oes.db.QueryRowContext(ctx, query, orderID).Scan(
		&order.ID,
		&order.ClientID,
		&order.Instrument,
		&sideStr,
		&typeStr,
		&priceStr,
		&quantityStr,
		&filledQtyStr,
		&statusStr,
		&order.CreatedAt,
		&order.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("order not found in snapshot: %s", orderID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get order from snapshot: %w", err)
	}

	// Parse fields
	order.Price, _ = decimal.NewFromString(priceStr)
	order.Quantity, _ = decimal.NewFromString(quantityStr)
	order.FilledQuantity, _ = decimal.NewFromString(filledQtyStr)
	order.Side = models.OrderSide(sideStr)
	order.Type = models.OrderType(typeStr)
	order.Status = models.OrderStatus(statusStr)

	return &order, nil
}

// GetOrdersByClientID retrieves all orders for a client from snapshot
func (oes *OrderEventStore) GetOrdersByClientID(ctx context.Context, clientID string, limit int) ([]*models.Order, error) {
	query := `
		SELECT order_id, client_id, instrument, side, type,
		       price, quantity, filled_quantity, status,
		       created_at, updated_at
		FROM orders_snapshot
		WHERE client_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := oes.db.QueryContext(ctx, query, clientID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get orders by client: %w", err)
	}
	defer rows.Close()

	var orders []*models.Order
	for rows.Next() {
		var order models.Order
		var priceStr, quantityStr, filledQtyStr string
		var sideStr, typeStr, statusStr string

		err := rows.Scan(
			&order.ID,
			&order.ClientID,
			&order.Instrument,
			&sideStr,
			&typeStr,
			&priceStr,
			&quantityStr,
			&filledQtyStr,
			&statusStr,
			&order.CreatedAt,
			&order.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}

		order.Price, _ = decimal.NewFromString(priceStr)
		order.Quantity, _ = decimal.NewFromString(quantityStr)
		order.FilledQuantity, _ = decimal.NewFromString(filledQtyStr)
		order.Side = models.OrderSide(sideStr)
		order.Type = models.OrderType(typeStr)
		order.Status = models.OrderStatus(statusStr)

		orders = append(orders, &order)
	}

	return orders, nil
}

// InconsistentOrder represents an order with mismatched snapshot/event state
type InconsistentOrder struct {
	OrderID        uuid.UUID
	SnapshotStatus string
	ReplayedStatus string
	SnapshotFilled decimal.Decimal
	ReplayedFilled decimal.Decimal
	IsConsistent   bool
}

// GetOrderStats returns statistics about order events
func (oes *OrderEventStore) GetOrderStats(ctx context.Context) (*OrderStats, error) {
	query := `
		SELECT 
			COUNT(DISTINCT order_id) as total_orders,
			COUNT(*) as total_events,
			COUNT(DISTINCT CASE WHEN event_type = 'ORDER_CREATED' THEN order_id END) as created_count,
			COUNT(DISTINCT CASE WHEN event_type = 'ORDER_FILLED' THEN order_id END) as filled_count,
			COUNT(DISTINCT CASE WHEN event_type = 'ORDER_CANCELLED' THEN order_id END) as cancelled_count
		FROM order_events
	`

	var stats OrderStats
	err := oes.db.QueryRowContext(ctx, query).Scan(
		&stats.TotalOrders,
		&stats.TotalEvents,
		&stats.CreatedCount,
		&stats.FilledCount,
		&stats.CancelledCount,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get order stats: %w", err)
	}

	return &stats, nil
}

// OrderStats contains statistics about order events
type OrderStats struct {
	TotalOrders     int64
	TotalEvents     int64
	CreatedCount    int64
	FilledCount     int64
	CancelledCount  int64
}
