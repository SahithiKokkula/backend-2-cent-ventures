package persistence

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestOrderEventStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	// Load event schema
	loadEventSchema(t, db)

	oes := NewOrderEventStore(db)
	ctx := context.Background()

	t.Run("RecordOrderCreated", func(t *testing.T) {
		order := models.NewOrder(
			"client1",
			"BTC-USD",
			models.OrderSideBuy,
			models.OrderTypeLimit,
			decimal.NewFromFloat(50000.0),
			decimal.NewFromFloat(1.0),
		)

		eventID, err := oes.RecordOrderCreated(ctx, order)
		if err != nil {
			t.Fatalf("Failed to record order created: %v", err)
		}

		if eventID == 0 {
			t.Error("Expected non-zero event ID")
		}

		// Verify order in snapshot
		snapshotOrder, err := oes.GetOrderFromSnapshot(ctx, order.ID)
		if err != nil {
			t.Fatalf("Failed to get order from snapshot: %v", err)
		}

		if snapshotOrder.Status != models.OrderStatusOpen {
			t.Errorf("Expected status open, got %v", snapshotOrder.Status)
		}

		t.Logf("✓ Order created with event ID: %d", eventID)
	})

	t.Run("RecordOrderFill", func(t *testing.T) {
		// Create order first
		order := models.NewOrder(
			"client2",
			"ETH-USD",
			models.OrderSideBuy,
			models.OrderTypeLimit,
			decimal.NewFromFloat(3000.0),
			decimal.NewFromFloat(10.0),
		)
		_, _ = oes.RecordOrderCreated(ctx, order)

		// Record partial fill
		tradeID := uuid.New()
		eventID, err := oes.RecordOrderFill(
			ctx,
			order.ID,
			decimal.NewFromFloat(5.0), // fill quantity
			decimal.NewFromFloat(5.0), // new filled quantity
			models.OrderStatusPartiallyFilled,
			&tradeID,
			time.Now(),
		)

		if err != nil {
			t.Fatalf("Failed to record order fill: %v", err)
		}

		// Verify snapshot updated
		snapshotOrder, err := oes.GetOrderFromSnapshot(ctx, order.ID)
		if err != nil {
			t.Fatalf("Failed to get order: %v", err)
		}

		if !snapshotOrder.FilledQuantity.Equal(decimal.NewFromFloat(5.0)) {
			t.Errorf("Expected filled quantity 5.0, got %s", snapshotOrder.FilledQuantity)
		}

		if snapshotOrder.Status != models.OrderStatusPartiallyFilled {
			t.Errorf("Expected status partially_filled, got %v", snapshotOrder.Status)
		}

		t.Logf("✓ Order fill recorded with event ID: %d", eventID)
	})

	t.Run("GetOrderHistory", func(t *testing.T) {
		// Create order with multiple events
		order := models.NewOrder(
			"client3",
			"BTC-USD",
			models.OrderSideSell,
			models.OrderTypeLimit,
			decimal.NewFromFloat(51000.0),
			decimal.NewFromFloat(2.0),
		)
		_, _ = oes.RecordOrderCreated(ctx, order)

		// Fill partially
		tradeID := uuid.New()
		_, _ = oes.RecordOrderFill(
			ctx,
			order.ID,
			decimal.NewFromFloat(1.0),
			decimal.NewFromFloat(1.0),
			models.OrderStatusPartiallyFilled,
			&tradeID,
			time.Now(),
		)

		// Fill completely
		tradeID2 := uuid.New()
		_, _ = oes.RecordOrderFill(
			ctx,
			order.ID,
			decimal.NewFromFloat(1.0),
			decimal.NewFromFloat(2.0),
			models.OrderStatusFilled,
			&tradeID2,
			time.Now(),
		)

		// Get history
		history, err := oes.GetOrderHistory(ctx, order.ID)
		if err != nil {
			t.Fatalf("Failed to get order history: %v", err)
		}

		if len(history) != 3 {
			t.Errorf("Expected 3 events, got %d", len(history))
		}

		// Verify event sequence
		if history[0].EventType != OrderEventCreated {
			t.Errorf("First event should be ORDER_CREATED, got %s", history[0].EventType)
		}

		if history[1].EventType != OrderEventPartiallyFilled {
			t.Errorf("Second event should be ORDER_PARTIALLY_FILLED, got %s", history[1].EventType)
		}

		if history[2].EventType != OrderEventFilled {
			t.Errorf("Third event should be ORDER_FILLED, got %s", history[2].EventType)
		}

		t.Logf("✓ Order history: %d events in correct sequence", len(history))
	})

	t.Run("ReplayOrderState", func(t *testing.T) {
		// Create order with events
		order := models.NewOrder(
			"client4",
			"SOL-USD",
			models.OrderSideBuy,
			models.OrderTypeLimit,
			decimal.NewFromFloat(100.0),
			decimal.NewFromFloat(20.0),
		)
		_, _ = oes.RecordOrderCreated(ctx, order)

		// Fill 15 units
		tradeID := uuid.New()
		_, _ = oes.RecordOrderFill(
			ctx,
			order.ID,
			decimal.NewFromFloat(15.0),
			decimal.NewFromFloat(15.0),
			models.OrderStatusPartiallyFilled,
			&tradeID,
			time.Now(),
		)

		// Replay state
		replayedOrder, err := oes.ReplayOrderState(ctx, order.ID)
		if err != nil {
			t.Fatalf("Failed to replay order state: %v", err)
		}

		// Verify replayed state matches
		if !replayedOrder.FilledQuantity.Equal(decimal.NewFromFloat(15.0)) {
			t.Errorf("Expected filled quantity 15.0, got %s", replayedOrder.FilledQuantity)
		}

		if replayedOrder.Status != models.OrderStatusPartiallyFilled {
			t.Errorf("Expected status partially_filled, got %v", replayedOrder.Status)
		}

		// Verify snapshot matches replayed state
		snapshotOrder, _ := oes.GetOrderFromSnapshot(ctx, order.ID)
		if !snapshotOrder.FilledQuantity.Equal(replayedOrder.FilledQuantity) {
			t.Error("Snapshot and replayed state mismatch")
		}

		t.Logf("✓ Order state replayed correctly from events")
	})

	t.Run("RecordOrderCancelled", func(t *testing.T) {
		order := models.NewOrder(
			"client5",
			"BTC-USD",
			models.OrderSideBuy,
			models.OrderTypeLimit,
			decimal.NewFromFloat(49000.0),
			decimal.NewFromFloat(0.5),
		)
		_, _ = oes.RecordOrderCreated(ctx, order)

		reason := "User requested cancellation"
		eventID, err := oes.RecordOrderCancelled(ctx, order.ID, &reason, time.Now())
		if err != nil {
			t.Fatalf("Failed to record cancellation: %v", err)
		}

		// Verify status updated
		snapshotOrder, _ := oes.GetOrderFromSnapshot(ctx, order.ID)
		if snapshotOrder.Status != models.OrderStatusCancelled {
			t.Errorf("Expected status cancelled, got %v", snapshotOrder.Status)
		}

		t.Logf("✓ Order cancelled with event ID: %d", eventID)
	})

	t.Run("RebuildSnapshot", func(t *testing.T) {
		// Count orders before rebuild
		stats, _ := oes.GetOrderStats(ctx)
		initialOrderCount := stats.TotalOrders

		// Rebuild snapshot
		count, err := oes.RebuildSnapshot(ctx)
		if err != nil {
			t.Fatalf("Failed to rebuild snapshot: %v", err)
		}

		if count != initialOrderCount {
			t.Errorf("Expected %d orders rebuilt, got %d", initialOrderCount, count)
		}

		t.Logf("✓ Snapshot rebuilt: %d orders", count)
	})

	t.Run("VerifyConsistency", func(t *testing.T) {
		inconsistencies, err := oes.VerifySnapshotConsistency(ctx)
		if err != nil {
			t.Fatalf("Failed to verify consistency: %v", err)
		}

		if len(inconsistencies) > 0 {
			t.Errorf("Found %d inconsistencies", len(inconsistencies))
			for _, inc := range inconsistencies {
				t.Logf("Inconsistent order %s: snapshot=%s replayed=%s",
					inc.OrderID, inc.SnapshotStatus, inc.ReplayedStatus)
			}
		} else {
			t.Logf("✓ All orders consistent between snapshot and events")
		}
	})

	t.Run("GetOrdersByClientID", func(t *testing.T) {
		// Create multiple orders for same client
		clientID := "client_test"
		for i := 0; i < 3; i++ {
			order := models.NewOrder(
				clientID,
				"BTC-USD",
				models.OrderSideBuy,
				models.OrderTypeLimit,
				decimal.NewFromFloat(50000.0+float64(i*1000)),
				decimal.NewFromFloat(1.0),
			)
			_, _ = oes.RecordOrderCreated(ctx, order)
		}

		// Retrieve orders
		orders, err := oes.GetOrdersByClientID(ctx, clientID, 10)
		if err != nil {
			t.Fatalf("Failed to get orders by client: %v", err)
		}

		if len(orders) < 3 {
			t.Errorf("Expected at least 3 orders, got %d", len(orders))
		}

		t.Logf("✓ Retrieved %d orders for client %s", len(orders), clientID)
	})

	t.Run("GetOrderStats", func(t *testing.T) {
		stats, err := oes.GetOrderStats(ctx)
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}

		t.Logf("✓ Order stats: %d total orders, %d total events", stats.TotalOrders, stats.TotalEvents)
		t.Logf("  Created: %d, Filled: %d, Cancelled: %d",
			stats.CreatedCount, stats.FilledCount, stats.CancelledCount)
	})
}

func TestEventSequencing(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	loadEventSchema(t, db)
	oes := NewOrderEventStore(db)
	ctx := context.Background()

	// Create order
	order := models.NewOrder(
		"sequence_test",
		"BTC-USD",
		models.OrderSideBuy,
		models.OrderTypeLimit,
		decimal.NewFromFloat(50000.0),
		decimal.NewFromFloat(10.0),
	)
	_, _ = oes.RecordOrderCreated(ctx, order)

	// Record multiple fills
	for i := 0; i < 5; i++ {
		tradeID := uuid.New()
		filledSoFar := decimal.NewFromFloat(float64(i + 1))
		status := models.OrderStatusPartiallyFilled
		if i == 4 {
			status = models.OrderStatusFilled
		}

		_, _ = oes.RecordOrderFill(
			ctx,
			order.ID,
			decimal.NewFromFloat(1.0),
			filledSoFar,
			status,
			&tradeID,
			time.Now(),
		)
	}

	// Verify event sequence
	history, err := oes.GetOrderHistory(ctx, order.ID)
	if err != nil {
		t.Fatalf("Failed to get history: %v", err)
	}

	if len(history) != 6 { // 1 create + 5 fills
		t.Errorf("Expected 6 events, got %d", len(history))
	}

	// Verify sequence numbers are sequential
	for i, event := range history {
		expectedSeq := int64(i + 1)
		if event.SequenceNumber != expectedSeq {
			t.Errorf("Event %d: expected sequence %d, got %d", i, expectedSeq, event.SequenceNumber)
		}
	}

	t.Logf("✓ Event sequencing verified: %d events with correct sequence numbers", len(history))
}

// Helper function to load event schema
func loadEventSchema(t *testing.T, db *sql.DB) {
	// In production, apply db/order_events_schema.sql before running tests
	// For testing, assume schema already exists
	t.Log("Note: Using existing schema from db/order_events_schema.sql")
}
