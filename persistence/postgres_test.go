package persistence

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

// MockDB setup for testing (requires Docker or local PostgreSQL)
func setupTestDB(t *testing.T) (*sql.DB, func()) {
	// Connection string for test database
	connStr := "postgres://postgres:postgres@localhost:5432/trading_engine_test?sslmode=disable"

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skip("PostgreSQL not available for testing:", err)
		return nil, nil
	}

	// Test connection
	if err := db.Ping(); err != nil {
		t.Skip("Cannot connect to PostgreSQL:", err)
		return nil, nil
	}

	// Cleanup function
	cleanup := func() {
		// Clean up test data
		_, _ = db.Exec("TRUNCATE trades, orders CASCADE")
		_ = db.Close()
	}

	// Create tables if they don't exist
	createTables(t, db)

	return db, cleanup
}

func createTables(t *testing.T, db *sql.DB) {
	schema := `
		CREATE TABLE IF NOT EXISTS orders (
			order_id UUID PRIMARY KEY,
			client_id VARCHAR(255) NOT NULL,
			instrument VARCHAR(50) NOT NULL,
			side VARCHAR(10) NOT NULL,
			type VARCHAR(10) NOT NULL,
			price NUMERIC(20, 8) NOT NULL,
			quantity NUMERIC(20, 8) NOT NULL,
			filled_quantity NUMERIC(20, 8) NOT NULL DEFAULT 0,
			status VARCHAR(20) NOT NULL,
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS trades (
			trade_id UUID PRIMARY KEY,
			buy_order_id UUID NOT NULL,
			sell_order_id UUID NOT NULL,
			instrument VARCHAR(50) NOT NULL,
			price NUMERIC(20, 8) NOT NULL,
			quantity NUMERIC(20, 8) NOT NULL,
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);
	`

	_, err := db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}
}

func TestPersistTradeWithOrders(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return // Test skipped
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ctx := context.Background()

	// Create test orders
	buyOrderID := uuid.New()
	sellOrderID := uuid.New()

	buyOrder := &models.Order{
		ID:             buyOrderID,
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	sellOrder := &models.Order{
		ID:             sellOrderID,
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Insert orders
	if err := ps.InsertOrder(ctx, buyOrder); err != nil {
		t.Fatalf("Failed to insert buy order: %v", err)
	}
	if err := ps.InsertOrder(ctx, sellOrder); err != nil {
		t.Fatalf("Failed to insert sell order: %v", err)
	}

	// Create trade record
	tradeID := uuid.New()
	trade := &TradeRecord{
		TradeID:     tradeID,
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Instrument:  "BTC-USD",
		Price:       decimal.NewFromFloat(50000.0),
		Quantity:    decimal.NewFromFloat(1.0),
		Timestamp:   time.Now(),
		CreatedAt:   time.Now(),
	}

	// Create order updates
	buyUpdate := &OrderUpdate{
		OrderID:        buyOrderID,
		FilledQuantity: decimal.NewFromFloat(1.0),
		Status:         models.OrderStatusFilled,
		UpdatedAt:      time.Now(),
	}

	sellUpdate := &OrderUpdate{
		OrderID:        sellOrderID,
		FilledQuantity: decimal.NewFromFloat(1.0),
		Status:         models.OrderStatusFilled,
		UpdatedAt:      time.Now(),
	}

	// Persist trade with order updates
	err := ps.PersistTradeWithOrders(ctx, trade, buyUpdate, sellUpdate)
	if err != nil {
		t.Fatalf("Failed to persist trade with orders: %v", err)
	}

	// Verify trade was persisted
	retrievedTrade, err := ps.GetTrade(ctx, tradeID)
	if err != nil {
		t.Fatalf("Failed to get trade: %v", err)
	}

	if !retrievedTrade.Price.Equal(trade.Price) {
		t.Errorf("Expected price %s, got %s", trade.Price, retrievedTrade.Price)
	}
	if !retrievedTrade.Quantity.Equal(trade.Quantity) {
		t.Errorf("Expected quantity %s, got %s", trade.Quantity, retrievedTrade.Quantity)
	}

	// Verify orders were updated
	retrievedBuyOrder, err := ps.GetOrder(ctx, buyOrderID)
	if err != nil {
		t.Fatalf("Failed to get buy order: %v", err)
	}

	if !retrievedBuyOrder.FilledQuantity.Equal(decimal.NewFromFloat(1.0)) {
		t.Errorf("Expected buy order filled quantity 1.0, got %s", retrievedBuyOrder.FilledQuantity)
	}
	if retrievedBuyOrder.Status != models.OrderStatusFilled {
		t.Errorf("Expected buy order status filled, got %v", retrievedBuyOrder.Status)
	}

	retrievedSellOrder, err := ps.GetOrder(ctx, sellOrderID)
	if err != nil {
		t.Fatalf("Failed to get sell order: %v", err)
	}

	if !retrievedSellOrder.FilledQuantity.Equal(decimal.NewFromFloat(1.0)) {
		t.Errorf("Expected sell order filled quantity 1.0, got %s", retrievedSellOrder.FilledQuantity)
	}
	if retrievedSellOrder.Status != models.OrderStatusFilled {
		t.Errorf("Expected sell order status filled, got %v", retrievedSellOrder.Status)
	}

	t.Logf("✓ Trade persisted with atomic order updates")
}

func TestTradeIdempotency(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ctx := context.Background()

	// Create and insert orders
	buyOrderID := uuid.New()
	sellOrderID := uuid.New()

	buyOrder := &models.Order{
		ID:             buyOrderID,
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	sellOrder := &models.Order{
		ID:             sellOrderID,
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_ = ps.InsertOrder(ctx, buyOrder)
	_ = ps.InsertOrder(ctx, sellOrder)

	// Create trade with same ID twice
	tradeID := uuid.New()
	trade := &TradeRecord{
		TradeID:     tradeID,
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Instrument:  "BTC-USD",
		Price:       decimal.NewFromFloat(50000.0),
		Quantity:    decimal.NewFromFloat(1.0),
		Timestamp:   time.Now(),
		CreatedAt:   time.Now(),
	}

	buyUpdate := &OrderUpdate{
		OrderID:        buyOrderID,
		FilledQuantity: decimal.NewFromFloat(1.0),
		Status:         models.OrderStatusFilled,
		UpdatedAt:      time.Now(),
	}

	sellUpdate := &OrderUpdate{
		OrderID:        sellOrderID,
		FilledQuantity: decimal.NewFromFloat(1.0),
		Status:         models.OrderStatusFilled,
		UpdatedAt:      time.Now(),
	}

	// First persistence
	err := ps.PersistTradeWithOrders(ctx, trade, buyUpdate, sellUpdate)
	if err != nil {
		t.Fatalf("Failed to persist trade first time: %v", err)
	}

	// Second persistence with same trade ID (should be idempotent)
	err = ps.PersistTradeWithOrders(ctx, trade, buyUpdate, sellUpdate)
	if err != nil {
		t.Fatalf("Failed to persist trade second time (idempotency): %v", err)
	}

	// Verify only one trade exists
	retrievedTrade, err := ps.GetTrade(ctx, tradeID)
	if err != nil {
		t.Fatalf("Failed to get trade: %v", err)
	}

	if retrievedTrade.TradeID != tradeID {
		t.Errorf("Trade ID mismatch")
	}

	t.Logf("✓ Trade idempotency verified - duplicate writes handled correctly")
}

func TestPartialFillPersistence(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ctx := context.Background()

	// Create orders
	buyOrderID := uuid.New()
	sellOrderID := uuid.New()

	buyOrder := &models.Order{
		ID:             buyOrderID,
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(10.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	sellOrder := &models.Order{
		ID:             sellOrderID,
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(5.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_ = ps.InsertOrder(ctx, buyOrder)
	_ = ps.InsertOrder(ctx, sellOrder)

	// First partial fill - 5.0 shares
	trade1 := &TradeRecord{
		TradeID:     uuid.New(),
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Instrument:  "BTC-USD",
		Price:       decimal.NewFromFloat(50000.0),
		Quantity:    decimal.NewFromFloat(5.0),
		Timestamp:   time.Now(),
		CreatedAt:   time.Now(),
	}

	buyUpdate1 := &OrderUpdate{
		OrderID:        buyOrderID,
		FilledQuantity: decimal.NewFromFloat(5.0),
		Status:         models.OrderStatusPartiallyFilled,
		UpdatedAt:      time.Now(),
	}

	sellUpdate1 := &OrderUpdate{
		OrderID:        sellOrderID,
		FilledQuantity: decimal.NewFromFloat(5.0),
		Status:         models.OrderStatusFilled,
		UpdatedAt:      time.Now(),
	}

	err := ps.PersistTradeWithOrders(ctx, trade1, buyUpdate1, sellUpdate1)
	if err != nil {
		t.Fatalf("Failed to persist first partial fill: %v", err)
	}

	// Verify first partial fill
	buyOrderAfterFirst, err := ps.GetOrder(ctx, buyOrderID)
	if err != nil {
		t.Fatalf("Failed to get buy order: %v", err)
	}

	if !buyOrderAfterFirst.FilledQuantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected filled quantity 5.0, got %s", buyOrderAfterFirst.FilledQuantity)
	}
	if buyOrderAfterFirst.Status != models.OrderStatusPartiallyFilled {
		t.Errorf("Expected status partially_filled, got %v", buyOrderAfterFirst.Status)
	}

	t.Logf("✓ Partial fill persistence verified")
}

func TestRetryLogic(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ps.SetRetryConfig(3, 10*time.Millisecond)

	ctx := context.Background()

	// Test retry on serialization failure (simulated)
	retryCount := 0
	maxRetries := 2

	err := ps.executeWithRetry(ctx, func(ctx context.Context) error {
		retryCount++
		if retryCount < maxRetries {
			// Simulate serialization failure
			return &pq.Error{Code: "40001"} // serialization_failure
		}
		return nil // Success on final retry
	})

	if err != nil {
		t.Fatalf("Expected success after retries, got error: %v", err)
	}

	if retryCount != maxRetries {
		t.Errorf("Expected %d retries, got %d", maxRetries, retryCount)
	}

	t.Logf("✓ Retry logic verified - %d retries executed", retryCount)
}

func TestRetryableErrors(t *testing.T) {
	ps := NewPostgresStore(nil)

	testCases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "Serialization failure",
			err:       &pq.Error{Code: "40001"},
			retryable: true,
		},
		{
			name:      "Deadlock detected",
			err:       &pq.Error{Code: "40P01"},
			retryable: true,
		},
		{
			name:      "Connection failure",
			err:       &pq.Error{Code: "08006"},
			retryable: true,
		},
		{
			name:      "Unique constraint violation",
			err:       &pq.Error{Code: "23505"},
			retryable: false,
		},
		{
			name:      "Not null violation",
			err:       &pq.Error{Code: "23502"},
			retryable: false,
		},
		{
			name:      "No error",
			err:       nil,
			retryable: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ps.isRetryableError(tc.err)
			if result != tc.retryable {
				t.Errorf("Expected retryable=%v, got %v for error: %v", tc.retryable, result, tc.err)
			}
		})
	}

	t.Logf("✓ Retryable error detection verified")
}

func TestPersistMultipleTrades(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ctx := context.Background()

	// Create 3 orders
	buy1 := uuid.New()
	sell1 := uuid.New()
	sell2 := uuid.New()

	orders := []*models.Order{
		{
			ID:             buy1,
			ClientID:       "buyer1",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideBuy,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(50000.0),
			Quantity:       decimal.NewFromFloat(10.0),
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		{
			ID:             sell1,
			ClientID:       "seller1",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideSell,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(49000.0),
			Quantity:       decimal.NewFromFloat(3.0),
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		{
			ID:             sell2,
			ClientID:       "seller2",
			Instrument:     "BTC-USD",
			Side:           models.OrderSideSell,
			Type:           models.OrderTypeLimit,
			Price:          decimal.NewFromFloat(49500.0),
			Quantity:       decimal.NewFromFloat(5.0),
			FilledQuantity: decimal.Zero,
			Status:         models.OrderStatusOpen,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
	}

	for _, order := range orders {
		_ = ps.InsertOrder(ctx, order)
	}

	// Create 2 trades
	trade1 := &TradeRecord{
		TradeID:     uuid.New(),
		BuyOrderID:  buy1,
		SellOrderID: sell1,
		Instrument:  "BTC-USD",
		Price:       decimal.NewFromFloat(49000.0),
		Quantity:    decimal.NewFromFloat(3.0),
		Timestamp:   time.Now(),
		CreatedAt:   time.Now(),
	}

	trade2 := &TradeRecord{
		TradeID:     uuid.New(),
		BuyOrderID:  buy1,
		SellOrderID: sell2,
		Instrument:  "BTC-USD",
		Price:       decimal.NewFromFloat(49500.0),
		Quantity:    decimal.NewFromFloat(5.0),
		Timestamp:   time.Now(),
		CreatedAt:   time.Now(),
	}

	// Order updates
	updates := []*OrderUpdate{
		{
			OrderID:        buy1,
			FilledQuantity: decimal.NewFromFloat(8.0),
			Status:         models.OrderStatusPartiallyFilled,
			UpdatedAt:      time.Now(),
		},
		{
			OrderID:        sell1,
			FilledQuantity: decimal.NewFromFloat(3.0),
			Status:         models.OrderStatusFilled,
			UpdatedAt:      time.Now(),
		},
		{
			OrderID:        sell2,
			FilledQuantity: decimal.NewFromFloat(5.0),
			Status:         models.OrderStatusFilled,
			UpdatedAt:      time.Now(),
		},
	}

	// Persist multiple trades in single transaction
	err := ps.PersistMultipleTrades(ctx, []*TradeRecord{trade1, trade2}, updates)
	if err != nil {
		t.Fatalf("Failed to persist multiple trades: %v", err)
	}

	// Verify both trades exist
	t1, err := ps.GetTrade(ctx, trade1.TradeID)
	if err != nil {
		t.Fatalf("Failed to get trade1: %v", err)
	}
	if !t1.Quantity.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected trade1 quantity 3.0, got %s", t1.Quantity)
	}

	t2, err := ps.GetTrade(ctx, trade2.TradeID)
	if err != nil {
		t.Fatalf("Failed to get trade2: %v", err)
	}
	if !t2.Quantity.Equal(decimal.NewFromFloat(5.0)) {
		t.Errorf("Expected trade2 quantity 5.0, got %s", t2.Quantity)
	}

	// Verify buy order has cumulative filled quantity
	buyOrder, err := ps.GetOrder(ctx, buy1)
	if err != nil {
		t.Fatalf("Failed to get buy order: %v", err)
	}
	if !buyOrder.FilledQuantity.Equal(decimal.NewFromFloat(8.0)) {
		t.Errorf("Expected buy order filled quantity 8.0, got %s", buyOrder.FilledQuantity)
	}

	t.Logf("✓ Multiple trades persisted atomically")
}

func TestGetTradesByInstrument(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ctx := context.Background()

	// Insert some trades (simplified - no order setup)
	for i := 0; i < 5; i++ {
		trade := &TradeRecord{
			TradeID:     uuid.New(),
			BuyOrderID:  uuid.New(),
			SellOrderID: uuid.New(),
			Instrument:  "BTC-USD",
			Price:       decimal.NewFromFloat(50000.0 + float64(i*100)),
			Quantity:    decimal.NewFromFloat(1.0),
			Timestamp:   time.Now().Add(time.Duration(i) * time.Second),
			CreatedAt:   time.Now(),
		}

		// Direct insert (bypassing order updates for this test)
		tx, _ := db.Begin()
		_ = ps.insertTrade(ctx, tx, trade)
		_ = tx.Commit()
	}

	// Get trades for instrument
	trades, err := ps.GetTradesByInstrument(ctx, "BTC-USD", 10)
	if err != nil {
		t.Fatalf("Failed to get trades: %v", err)
	}

	if len(trades) != 5 {
		t.Errorf("Expected 5 trades, got %d", len(trades))
	}

	// Verify they're in descending order by timestamp
	for i := 0; i < len(trades)-1; i++ {
		if trades[i].Timestamp.Before(trades[i+1].Timestamp) {
			t.Error("Trades not in descending timestamp order")
		}
	}

	t.Logf("✓ Retrieved %d trades for instrument", len(trades))
}

func TestTransactionCrashNoPartialState(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ctx := context.Background()

	// Create orders
	buyOrderID := uuid.New()
	sellOrderID := uuid.New()

	buyOrder := &models.Order{
		ID:             buyOrderID,
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	sellOrder := &models.Order{
		ID:             sellOrderID,
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_ = ps.InsertOrder(ctx, buyOrder)
	_ = ps.InsertOrder(ctx, sellOrder)

	tradeID := uuid.New()

	// Simulate crash by NOT committing transaction
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// Insert trade but don't commit
	trade := &TradeRecord{
		TradeID:     tradeID,
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Instrument:  "BTC-USD",
		Price:       decimal.NewFromFloat(50000.0),
		Quantity:    decimal.NewFromFloat(1.0),
		Timestamp:   time.Now(),
		CreatedAt:   time.Now(),
	}

	err = ps.insertTrade(ctx, tx, trade)
	if err != nil {
		t.Fatalf("Failed to insert trade: %v", err)
	}

	// Simulate crash - rollback instead of commit
	_ = tx.Rollback() // Ignore rollback error in test
	t.Logf("Transaction rolled back (simulated crash)")

	// Verify trade does NOT exist in database
	_, err = ps.GetTrade(ctx, tradeID)
	if err == nil {
		t.Errorf("Expected trade to NOT exist after rollback, but it was found")
	}
	if err != sql.ErrNoRows {
		t.Logf("Expected sql.ErrNoRows, got: %v", err)
	}

	// Verify orders still in original state
	retrievedBuyOrder, err := ps.GetOrder(ctx, buyOrderID)
	if err != nil {
		t.Fatalf("Failed to get buy order: %v", err)
	}

	if !retrievedBuyOrder.FilledQuantity.Equal(decimal.Zero) {
		t.Errorf("Expected buy order filled quantity 0, got %s (partial state leaked!)", retrievedBuyOrder.FilledQuantity)
	}
	if retrievedBuyOrder.Status != models.OrderStatusOpen {
		t.Errorf("Expected buy order status open, got %v (partial state leaked!)", retrievedBuyOrder.Status)
	}

	t.Logf("✓ No partial state after transaction rollback")
}

func TestTransactionRollbackOnError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ctx := context.Background()

	// Create orders
	buyOrderID := uuid.New()
	sellOrderID := uuid.New()

	buyOrder := &models.Order{
		ID:             buyOrderID,
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	sellOrder := &models.Order{
		ID:             sellOrderID,
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_ = ps.InsertOrder(ctx, buyOrder)
	_ = ps.InsertOrder(ctx, sellOrder)

	// Create valid trade
	trade := &TradeRecord{
		TradeID:     uuid.New(),
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Instrument:  "BTC-USD",
		Price:       decimal.NewFromFloat(50000.0),
		Quantity:    decimal.NewFromFloat(1.0),
		Timestamp:   time.Now(),
		CreatedAt:   time.Now(),
	}

	// Create INVALID order update (non-existent order)
	invalidOrderID := uuid.New()
	buyUpdate := &OrderUpdate{
		OrderID:        buyOrderID,
		FilledQuantity: decimal.NewFromFloat(1.0),
		Status:         models.OrderStatusFilled,
		UpdatedAt:      time.Now(),
	}

	invalidUpdate := &OrderUpdate{
		OrderID:        invalidOrderID, // This order doesn't exist!
		FilledQuantity: decimal.NewFromFloat(1.0),
		Status:         models.OrderStatusFilled,
		UpdatedAt:      time.Now(),
	}

	// Attempt to persist - should fail and rollback
	err := ps.PersistTradeWithOrders(ctx, trade, buyUpdate, invalidUpdate)
	if err == nil {
		t.Fatalf("Expected error when updating non-existent order, but got none")
	}
	t.Logf("Got expected error: %v", err)

	// Verify trade was NOT persisted (rolled back)
	_, err = ps.GetTrade(ctx, trade.TradeID)
	if err == nil {
		t.Errorf("Trade should not exist after transaction rollback")
	}

	// Verify buy order was NOT updated (rolled back)
	retrievedBuyOrder, err := ps.GetOrder(ctx, buyOrderID)
	if err != nil {
		t.Fatalf("Failed to get buy order: %v", err)
	}

	if !retrievedBuyOrder.FilledQuantity.Equal(decimal.Zero) {
		t.Errorf("Buy order should still have filled_quantity=0 after rollback, got %s", retrievedBuyOrder.FilledQuantity)
	}

	t.Logf("✓ Transaction rolled back on error - no partial state")
}

func TestConcurrentOrderUpdates(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ctx := context.Background()

	// Create a single order
	orderID := uuid.New()
	order := &models.Order{
		ID:             orderID,
		ClientID:       "trader1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(10.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_ = ps.InsertOrder(ctx, order)

	// Simulate two concurrent updates to the same order
	done := make(chan error, 2)

	// Goroutine 1: Update to 5.0 filled
	go func() {
		tx1, _ := db.Begin()
		update1 := &OrderUpdate{
			OrderID:        orderID,
			FilledQuantity: decimal.NewFromFloat(5.0),
			Status:         models.OrderStatusPartiallyFilled,
			UpdatedAt:      time.Now(),
		}
		err := ps.updateOrder(ctx, tx1, update1)
		if err == nil {
			time.Sleep(50 * time.Millisecond) // Hold lock briefly
			err = tx1.Commit()
		} else {
			_ = tx1.Rollback()
		}
		done <- err
	}()

	// Goroutine 2: Update to 8.0 filled (starts slightly later)
	go func() {
		time.Sleep(10 * time.Millisecond) // Start after goroutine 1
		tx2, _ := db.Begin()
		update2 := &OrderUpdate{
			OrderID:        orderID,
			FilledQuantity: decimal.NewFromFloat(8.0),
			Status:         models.OrderStatusPartiallyFilled,
			UpdatedAt:      time.Now(),
		}
		err := ps.updateOrder(ctx, tx2, update2)
		if err == nil {
			err = tx2.Commit()
		} else {
			_ = tx2.Rollback()
		}
		done <- err
	}()

	// Wait for both to complete
	err1 := <-done
	err2 := <-done

	if err1 != nil && err2 != nil {
		t.Fatalf("Both transactions failed: %v, %v", err1, err2)
	}

	// One should succeed
	t.Logf("Transaction results: err1=%v, err2=%v", err1, err2)

	// Verify final state is consistent
	finalOrder, err := ps.GetOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("Failed to get order: %v", err)
	}

	// Should be either 5.0 or 8.0 (whichever committed last)
	if !finalOrder.FilledQuantity.Equal(decimal.NewFromFloat(5.0)) &&
		!finalOrder.FilledQuantity.Equal(decimal.NewFromFloat(8.0)) {
		t.Errorf("Expected filled quantity 5.0 or 8.0, got %s", finalOrder.FilledQuantity)
	}

	t.Logf("✓ Concurrent updates handled correctly - final filled_quantity: %s", finalOrder.FilledQuantity)
}

func TestDataConsistencyCheck(t *testing.T) {
	db, cleanup := setupTestDB(t)
	if cleanup == nil {
		return
	}
	defer cleanup()

	ps := NewPostgresStore(db)
	ctx := context.Background()

	// Create and persist valid trade
	buyOrderID := uuid.New()
	sellOrderID := uuid.New()

	buyOrder := &models.Order{
		ID:             buyOrderID,
		ClientID:       "buyer1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideBuy,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	sellOrder := &models.Order{
		ID:             sellOrderID,
		ClientID:       "seller1",
		Instrument:     "BTC-USD",
		Side:           models.OrderSideSell,
		Type:           models.OrderTypeLimit,
		Price:          decimal.NewFromFloat(50000.0),
		Quantity:       decimal.NewFromFloat(1.0),
		FilledQuantity: decimal.Zero,
		Status:         models.OrderStatusOpen,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_ = ps.InsertOrder(ctx, buyOrder)
	_ = ps.InsertOrder(ctx, sellOrder)

	trade := &TradeRecord{
		TradeID:     uuid.New(),
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Instrument:  "BTC-USD",
		Price:       decimal.NewFromFloat(50000.0),
		Quantity:    decimal.NewFromFloat(1.0),
		Timestamp:   time.Now(),
		CreatedAt:   time.Now(),
	}

	buyUpdate := &OrderUpdate{
		OrderID:        buyOrderID,
		FilledQuantity: decimal.NewFromFloat(1.0),
		Status:         models.OrderStatusFilled,
		UpdatedAt:      time.Now(),
	}

	sellUpdate := &OrderUpdate{
		OrderID:        sellOrderID,
		FilledQuantity: decimal.NewFromFloat(1.0),
		Status:         models.OrderStatusFilled,
		UpdatedAt:      time.Now(),
	}

	_ = ps.PersistTradeWithOrders(ctx, trade, buyUpdate, sellUpdate)

	// Run consistency checks
	var orphanedCount int

	// Check 1: No orphaned filled orders
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM orders o
		LEFT JOIN trades t ON (o.order_id = t.buy_order_id OR o.order_id = t.sell_order_id)
		WHERE o.status = 'filled' AND t.trade_id IS NULL
	`).Scan(&orphanedCount)

	if err != nil {
		t.Fatalf("Failed to check orphaned orders: %v", err)
	}
	if orphanedCount > 0 {
		t.Errorf("Found %d orphaned filled orders (should be 0)", orphanedCount)
	}

	// Check 2: No overfilled orders
	var overfilledCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM orders
		WHERE filled_quantity > quantity
	`).Scan(&overfilledCount)

	if err != nil {
		t.Fatalf("Failed to check overfilled orders: %v", err)
	}
	if overfilledCount > 0 {
		t.Errorf("Found %d overfilled orders (should be 0)", overfilledCount)
	}

	// Check 3: No negative quantities
	var negativeCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM trades
		WHERE quantity <= 0
	`).Scan(&negativeCount)

	if err != nil {
		t.Fatalf("Failed to check negative quantities: %v", err)
	}
	if negativeCount > 0 {
		t.Errorf("Found %d trades with negative/zero quantity (should be 0)", negativeCount)
	}

	t.Logf("✓ Data consistency verified - all checks passed")
}
