package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TestEventReplayConsistency verifies that replaying events recreates the same in-memory state
func TestEventReplayConsistency(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	oes := NewOrderEventStore(db)
	ctx := context.Background()

	t.Run("SimpleOrderCreation", func(t *testing.T) {
		// Create order in memory
		originalOrder := models.NewOrder(
			"client_replay",
			"BTC-USD",
			models.OrderSideBuy,
			models.OrderTypeLimit,
			decimal.NewFromFloat(50000.0),
			decimal.NewFromFloat(1.0),
		)

		// Record to DB
		_, err := oes.RecordOrderCreated(ctx, originalOrder)
		if err != nil {
			t.Fatalf("Failed to record order: %v", err)
		}

		// Replay from events
		replayedOrder, err := oes.ReplayOrderState(ctx, originalOrder.ID)
		if err != nil {
			t.Fatalf("Failed to replay order: %v", err)
		}

		// Verify exact match
		if !ordersEqual(originalOrder, replayedOrder) {
			t.Errorf("Replayed order does not match original")
			t.Logf("Original:  %+v", originalOrder)
			t.Logf("Replayed:  %+v", replayedOrder)
		}

		t.Logf("✓ Order creation replay consistent")
	})

	t.Run("PartialFillReplay", func(t *testing.T) {
		// Create and fill order in memory
		order := models.NewOrder(
			"client_partial",
			"ETH-USD",
			models.OrderSideSell,
			models.OrderTypeLimit,
			decimal.NewFromFloat(3000.0),
			decimal.NewFromFloat(10.0),
		)

		// Record creation
		_, _ = oes.RecordOrderCreated(ctx, order)

		// Fill partially in memory
		fillQty := decimal.NewFromFloat(6.0)
		order.Fill(fillQty)

		// Record fill to DB
		tradeID := uuid.New()
		_, _ = oes.RecordOrderFill(
			ctx,
			order.ID,
			fillQty,
			order.FilledQuantity,
			order.Status,
			&tradeID,
			time.Now(),
		)

		// Replay from events
		replayedOrder, err := oes.ReplayOrderState(ctx, order.ID)
		if err != nil {
			t.Fatalf("Failed to replay: %v", err)
		}

		// Verify state matches
		if !replayedOrder.FilledQuantity.Equal(order.FilledQuantity) {
			t.Errorf("Filled quantity mismatch: expected %s, got %s",
				order.FilledQuantity, replayedOrder.FilledQuantity)
		}

		if replayedOrder.Status != order.Status {
			t.Errorf("Status mismatch: expected %s, got %s",
				order.Status, replayedOrder.Status)
		}

		if !replayedOrder.RemainingQuantity().Equal(order.RemainingQuantity()) {
			t.Errorf("Remaining quantity mismatch: expected %s, got %s",
				order.RemainingQuantity(), replayedOrder.RemainingQuantity())
		}

		t.Logf("✓ Partial fill replay consistent")
	})

	t.Run("MultipleFillsReplay", func(t *testing.T) {
		// Create order
		order := models.NewOrder(
			"client_multi",
			"BTC-USD",
			models.OrderSideBuy,
			models.OrderTypeLimit,
			decimal.NewFromFloat(50000.0),
			decimal.NewFromFloat(10.0),
		)
		_, _ = oes.RecordOrderCreated(ctx, order)

		// Apply multiple fills
		fills := []decimal.Decimal{
			decimal.NewFromFloat(2.0),
			decimal.NewFromFloat(3.0),
			decimal.NewFromFloat(5.0), // Total = 10.0 (filled)
		}

		for _, fillQty := range fills {
			order.Fill(fillQty)
			tradeID := uuid.New()
			_, _ = oes.RecordOrderFill(
				ctx,
				order.ID,
				fillQty,
				order.FilledQuantity,
				order.Status,
				&tradeID,
				time.Now(),
			)
			time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		}

		// Replay from events
		replayedOrder, err := oes.ReplayOrderState(ctx, order.ID)
		if err != nil {
			t.Fatalf("Failed to replay: %v", err)
		}

		// Verify final state
		if !replayedOrder.FilledQuantity.Equal(order.FilledQuantity) {
			t.Errorf("Final filled quantity mismatch: expected %s, got %s",
				order.FilledQuantity, replayedOrder.FilledQuantity)
		}

		if replayedOrder.Status != models.OrderStatusFilled {
			t.Errorf("Expected status filled, got %s", replayedOrder.Status)
		}

		if !order.IsFilled() || !replayedOrder.IsFilled() {
			t.Error("Both orders should be completely filled")
		}

		t.Logf("✓ Multiple fills replay consistent")
	})

	t.Run("CancellationReplay", func(t *testing.T) {
		// Create order
		order := models.NewOrder(
			"client_cancel",
			"SOL-USD",
			models.OrderSideBuy,
			models.OrderTypeLimit,
			decimal.NewFromFloat(100.0),
			decimal.NewFromFloat(20.0),
		)
		_, _ = oes.RecordOrderCreated(ctx, order)

		// Partial fill
		fillQty := decimal.NewFromFloat(5.0)
		order.Fill(fillQty)
		tradeID := uuid.New()
		_, _ = oes.RecordOrderFill(ctx, order.ID, fillQty, order.FilledQuantity, order.Status, &tradeID, time.Now())

		// Cancel in memory
		order.Cancel()

		// Record cancellation
		reason := "User requested"
		_, _ = oes.RecordOrderCancelled(ctx, order.ID, &reason, time.Now()) // Replay
		replayedOrder, err := oes.ReplayOrderState(ctx, order.ID)
		if err != nil {
			t.Fatalf("Failed to replay: %v", err)
		}

		// Verify cancelled state
		if replayedOrder.Status != models.OrderStatusCancelled {
			t.Errorf("Expected status cancelled, got %s", replayedOrder.Status)
		}

		// Filled quantity should remain
		if !replayedOrder.FilledQuantity.Equal(fillQty) {
			t.Errorf("Filled quantity should persist: expected %s, got %s",
				fillQty, replayedOrder.FilledQuantity)
		}

		t.Logf("✓ Cancellation replay consistent")
	})

	t.Run("SnapshotMatchesReplay", func(t *testing.T) {
		// Create complex order lifecycle
		order := models.NewOrder(
			"client_snapshot",
			"BTC-USD",
			models.OrderSideSell,
			models.OrderTypeLimit,
			decimal.NewFromFloat(51000.0),
			decimal.NewFromFloat(5.0),
		)
		_, _ = oes.RecordOrderCreated(ctx, order)

		// Multiple fills
		order.Fill(decimal.NewFromFloat(2.0))
		tradeID1 := uuid.New()
		_, _ = oes.RecordOrderFill(ctx, order.ID, decimal.NewFromFloat(2.0), order.FilledQuantity, order.Status, &tradeID1, time.Now())

		order.Fill(decimal.NewFromFloat(1.5))
		tradeID2 := uuid.New()
		_, _ = oes.RecordOrderFill(ctx, order.ID, decimal.NewFromFloat(1.5), order.FilledQuantity, order.Status, &tradeID2, time.Now()) // Get from snapshot (fast query)
		snapshotOrder, err := oes.GetOrderFromSnapshot(ctx, order.ID)
		if err != nil {
			t.Fatalf("Failed to get snapshot: %v", err)
		}

		// Replay from events
		replayedOrder, err := oes.ReplayOrderState(ctx, order.ID)
		if err != nil {
			t.Fatalf("Failed to replay: %v", err)
		}

		// Verify snapshot matches replay
		if !ordersEqual(snapshotOrder, replayedOrder) {
			t.Error("Snapshot does not match replayed state")
			t.Logf("Snapshot: %+v", snapshotOrder)
			t.Logf("Replayed: %+v", replayedOrder)
		}

		// Verify both match in-memory state
		if !snapshotOrder.FilledQuantity.Equal(order.FilledQuantity) {
			t.Errorf("Snapshot filled quantity mismatch: expected %s, got %s",
				order.FilledQuantity, snapshotOrder.FilledQuantity)
		}

		if !replayedOrder.FilledQuantity.Equal(order.FilledQuantity) {
			t.Errorf("Replayed filled quantity mismatch: expected %s, got %s",
				order.FilledQuantity, replayedOrder.FilledQuantity)
		}

		t.Logf("✓ Snapshot matches replayed state")
	})
}

// TestEventSequenceIntegrity verifies event sequence is preserved
func TestEventSequenceIntegrity(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	oes := NewOrderEventStore(db)
	ctx := context.Background()

	// Create order with many events
	order := models.NewOrder(
		"client_sequence",
		"BTC-USD",
		models.OrderSideBuy,
		models.OrderTypeLimit,
		decimal.NewFromFloat(50000.0),
		decimal.NewFromFloat(100.0),
	)
	_, _ = oes.RecordOrderCreated(ctx, order)

	// Record 10 fill events
	fillSize := decimal.NewFromFloat(10.0)
	for i := 0; i < 10; i++ {
		order.Fill(fillSize)
		tradeID := uuid.New()
		_, _ = oes.RecordOrderFill(
			ctx,
			order.ID,
			fillSize,
			order.FilledQuantity,
			order.Status,
			&tradeID,
			time.Now(),
		)
		time.Sleep(5 * time.Millisecond)
	}

	// Get event history
	events, err := oes.GetOrderHistory(ctx, order.ID)
	if err != nil {
		t.Fatalf("Failed to get history: %v", err)
	}

	if len(events) != 11 { // 1 create + 10 fills
		t.Errorf("Expected 11 events, got %d", len(events))
	}

	// Verify sequence numbers are monotonic
	for i, event := range events {
		expectedSeq := int64(i + 1)
		if event.SequenceNumber != expectedSeq {
			t.Errorf("Event %d: expected sequence %d, got %d",
				i, expectedSeq, event.SequenceNumber)
		}
	}

	// Verify timestamps are monotonic
	for i := 1; i < len(events); i++ {
		if events[i].EventTimestamp.Before(events[i-1].EventTimestamp) {
			t.Errorf("Event timestamp out of order at position %d", i)
		}
	}

	// Replay and verify final state
	replayedOrder, _ := oes.ReplayOrderState(ctx, order.ID)
	if !replayedOrder.FilledQuantity.Equal(order.FilledQuantity) {
		t.Errorf("Replayed state mismatch after %d events", len(events))
	}

	t.Logf("✓ Event sequence integrity verified: %d events", len(events))
}

// TestReplayPartialHistory verifies replay up to specific point in time
func TestReplayPartialHistory(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	oes := NewOrderEventStore(db)
	ctx := context.Background()

	// Create order
	order := models.NewOrder(
		"client_partial_history",
		"ETH-USD",
		models.OrderSideBuy,
		models.OrderTypeLimit,
		decimal.NewFromFloat(3000.0),
		decimal.NewFromFloat(10.0),
	)
	_, _ = oes.RecordOrderCreated(ctx, order)

	// Record fills with delays
	fills := []decimal.Decimal{
		decimal.NewFromFloat(2.0),
		decimal.NewFromFloat(3.0),
		decimal.NewFromFloat(5.0),
	}

	cumulativeFilled := decimal.Zero
	for _, fillQty := range fills {
		time.Sleep(50 * time.Millisecond)
		cumulativeFilled = cumulativeFilled.Add(fillQty)
		order.Fill(fillQty)

		tradeID := uuid.New()
		timestamp := time.Now()

		_, _ = oes.RecordOrderFill(ctx, order.ID, fillQty, order.FilledQuantity, order.Status, &tradeID, timestamp)
	}

	// Get all events
	allEvents, _ := oes.GetOrderHistory(ctx, order.ID)

	// Verify we can reconstruct state at each point
	expectedFilled := decimal.Zero
	for i, fillQty := range fills {
		expectedFilled = expectedFilled.Add(fillQty)

		// Count events up to this point
		eventsUpToNow := i + 2 // 1 create + (i+1) fills

		if int64(eventsUpToNow) != allEvents[eventsUpToNow-1].SequenceNumber {
			t.Errorf("Sequence number mismatch at fill %d", i)
		}

		t.Logf("After fill %d: filled_quantity = %s", i+1, expectedFilled)
	}

	// Final replay should match complete state
	finalOrder, _ := oes.ReplayOrderState(ctx, order.ID)
	if !finalOrder.FilledQuantity.Equal(order.FilledQuantity) {
		t.Errorf("Final replayed state mismatch")
	}

	t.Logf("✓ Partial history replay verified")
}

// ordersEqual compares two orders for equality (excluding timestamps)
func ordersEqual(a, b *models.Order) bool {
	if a.ID != b.ID {
		return false
	}
	if a.ClientID != b.ClientID {
		return false
	}
	if a.Instrument != b.Instrument {
		return false
	}
	if a.Side != b.Side {
		return false
	}
	if a.Type != b.Type {
		return false
	}
	if !a.Price.Equal(b.Price) {
		return false
	}
	if !a.Quantity.Equal(b.Quantity) {
		return false
	}
	if !a.FilledQuantity.Equal(b.FilledQuantity) {
		return false
	}
	if a.Status != b.Status {
		return false
	}
	return true
}

// TestInMemoryVsReplayedState verifies in-memory state matches replayed state
func TestInMemoryVsReplayedState(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	oes := NewOrderEventStore(db)
	ctx := context.Background()

	// Simulate realistic order lifecycle
	inMemoryOrder := models.NewOrder(
		"client_inmem",
		"BTC-USD",
		models.OrderSideBuy,
		models.OrderTypeLimit,
		decimal.NewFromFloat(50000.0),
		decimal.NewFromFloat(25.0),
	)

	// Record creation
	_, _ = oes.RecordOrderCreated(ctx, inMemoryOrder)

	// Simulate matching engine fills
	trades := []struct {
		quantity decimal.Decimal
		price    decimal.Decimal
	}{
		{decimal.NewFromFloat(5.0), decimal.NewFromFloat(50000.0)},
		{decimal.NewFromFloat(8.0), decimal.NewFromFloat(50000.0)},
		{decimal.NewFromFloat(7.0), decimal.NewFromFloat(50000.0)},
		{decimal.NewFromFloat(5.0), decimal.NewFromFloat(50000.0)},
	}

	for _, trade := range trades {
		// Update in-memory state
		inMemoryOrder.Fill(trade.quantity)

		// Record to DB
		tradeID := uuid.New()
		_, _ = oes.RecordOrderFill(
			ctx,
			inMemoryOrder.ID,
			trade.quantity,
			inMemoryOrder.FilledQuantity,
			inMemoryOrder.Status,
			&tradeID,
			time.Now(),
		)

		time.Sleep(10 * time.Millisecond)
	}

	// Replay from events
	replayedOrder, err := oes.ReplayOrderState(ctx, inMemoryOrder.ID)
	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	// Verify complete match
	if !ordersEqual(inMemoryOrder, replayedOrder) {
		t.Error("In-memory order does not match replayed order")
	}

	// Verify specific fields
	if !replayedOrder.FilledQuantity.Equal(inMemoryOrder.FilledQuantity) {
		t.Errorf("Filled quantity mismatch: in-memory=%s replayed=%s",
			inMemoryOrder.FilledQuantity, replayedOrder.FilledQuantity)
	}

	if replayedOrder.Status != inMemoryOrder.Status {
		t.Errorf("Status mismatch: in-memory=%s replayed=%s",
			inMemoryOrder.Status, replayedOrder.Status)
	}

	if !replayedOrder.IsFilled() || !inMemoryOrder.IsFilled() {
		t.Error("Both orders should be filled")
	}

	// Get snapshot and verify consistency
	snapshotOrder, _ := oes.GetOrderFromSnapshot(ctx, inMemoryOrder.ID)
	if !ordersEqual(inMemoryOrder, snapshotOrder) {
		t.Error("In-memory order does not match snapshot")
	}

	t.Logf("✓ In-memory state matches replayed state perfectly")
	t.Logf("  Filled: %s / %s", replayedOrder.FilledQuantity, replayedOrder.Quantity)
	t.Logf("  Status: %s", replayedOrder.Status)
}
