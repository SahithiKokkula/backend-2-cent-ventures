package eventsourcing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ============================================
// Replay Engine (Event Replay & Time Travel)
// ============================================

// ReplayEngine rebuilds state by replaying events
type ReplayEngine struct {
	store Store
}

// NewReplayEngine creates a new replay engine
func NewReplayEngine(store Store) *ReplayEngine {
	return &ReplayEngine{store: store}
}

// ReplayOrder rebuilds an order's state from events
func (r *ReplayEngine) ReplayOrder(ctx context.Context, orderID uuid.UUID) (*OrderState, error) {
	// Load all events for this order
	events, err := r.store.LoadEvents(ctx, orderID.String(), "Order")
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	// Initialize empty state
	state := &OrderState{}

	// Replay each event to rebuild state
	for _, event := range events {
		state.Apply(event)
	}

	return state, nil
}

// ReplayOrderToVersion rebuilds order state up to a specific version
func (r *ReplayEngine) ReplayOrderToVersion(ctx context.Context, orderID uuid.UUID, targetVersion int) (*OrderState, error) {
	events, err := r.store.LoadEvents(ctx, orderID.String(), "Order")
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}

	state := &OrderState{}

	// Replay events up to target version
	for _, event := range events {
		if event.GetVersion() > targetVersion {
			break
		}
		state.Apply(event)
	}

	return state, nil
}

// ReplayOrderToTimestamp rebuilds order state up to a specific point in time
func (r *ReplayEngine) ReplayOrderToTimestamp(ctx context.Context, orderID uuid.UUID, targetTime time.Time) (*OrderState, error) {
	events, err := r.store.LoadEvents(ctx, orderID.String(), "Order")
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}

	state := &OrderState{}

	// Replay events until we reach target time
	for _, event := range events {
		if event.GetTimestamp().After(targetTime) {
			break // Stop at target time
		}
		state.Apply(event)
	}

	return state, nil
}

// ============================================
// Order State (Rebuilt from Events)
// ============================================

// OrderState represents the current state of an order
type OrderState struct {
	OrderID         uuid.UUID
	ClientID        string
	Instrument      string
	Side            string
	Type            string
	Price           float64
	OriginalQty     float64
	RemainingQty    float64
	FilledQty       float64
	AverageFillPrice float64
	Status          string // "open", "partial", "filled", "cancelled"
	TimeInForce     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Version         int
}

// Apply applies an event to update the order state
func (s *OrderState) Apply(event Event) {
	switch e := event.(type) {
	case *OrderPlacedEvent:
		s.OrderID = e.OrderID
		s.ClientID = e.ClientID
		s.Instrument = e.Instrument
		s.Side = e.Side
		s.Type = e.Type
		s.Price = e.Price
		s.OriginalQty = e.Quantity
		s.RemainingQty = e.Quantity
		s.FilledQty = 0
		s.Status = "open"
		s.TimeInForce = e.TimeInForce
		s.CreatedAt = e.Timestamp
		s.UpdatedAt = e.Timestamp
		s.Version = e.Version

	case *OrderCancelledEvent:
		s.Status = "cancelled"
		s.UpdatedAt = e.Timestamp
		s.Version = e.Version

	case *OrderPartiallyFilledEvent:
		s.FilledQty += e.FilledQuantity
		s.RemainingQty = e.RemainingQuantity
		s.Status = "partial"
		s.UpdatedAt = e.Timestamp
		s.Version = e.Version

		// Update average fill price
		if s.FilledQty > 0 {
			s.AverageFillPrice = (s.AverageFillPrice*(s.FilledQty-e.FilledQuantity) + e.FillPrice*e.FilledQuantity) / s.FilledQty
		}

	case *OrderFilledEvent:
		s.FilledQty = e.TotalFilled
		s.RemainingQty = 0
		s.Status = "filled"
		s.AverageFillPrice = e.AveragePrice
		s.UpdatedAt = e.Timestamp
		s.Version = e.Version
	}
}

// ============================================
// Usage Example
// ============================================

/*
Example usage in your matching engine:

1. When order is placed:
   event := eventsourcing.NewOrderPlacedEvent(orderID, clientID, "BTC-USD", "buy", "limit", 50000, 1.5, "GTC", 1)
   eventStore.Append(ctx, event)

2. When order is cancelled:
   event := eventsourcing.NewOrderCancelledEvent(orderID, "user_requested", 2)
   eventStore.Append(ctx, event)

3. To rebuild current state:
   replayEngine := eventsourcing.NewReplayEngine(eventStore)
   orderState, _ := replayEngine.ReplayOrder(ctx, orderID)
   fmt.Printf("Order status: %s, filled: %.2f/%.2f\n", orderState.Status, orderState.FilledQty, orderState.OriginalQty)

4. Time travel (debug):
   // What was the order state 5 minutes ago?
   pastState, _ := replayEngine.ReplayOrderToTimestamp(ctx, orderID, time.Now().Add(-5*time.Minute))
   fmt.Printf("5 minutes ago, order was: %s\n", pastState.Status)
*/
