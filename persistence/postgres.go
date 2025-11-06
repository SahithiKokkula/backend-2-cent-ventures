package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/SahithiKokkula/backend-2-cent-ventures/logging"
	"github.com/SahithiKokkula/backend-2-cent-ventures/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

// Error types for retry logic
var (
	ErrDeadlock             = errors.New("deadlock detected")
	ErrSerializationFailure = errors.New("serialization failure")
	ErrConnectionFailure    = errors.New("connection failure")
)

// PostgresStore handles all database operations for the trading engine
type PostgresStore struct {
	db         *sql.DB
	maxRetries int
	retryDelay time.Duration
}

// NewPostgresStore creates a new PostgreSQL store
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{
		db:         db,
		maxRetries: 3,
		retryDelay: 100 * time.Millisecond,
	}
}

// SetRetryConfig sets the retry configuration
func (ps *PostgresStore) SetRetryConfig(maxRetries int, retryDelay time.Duration) {
	ps.maxRetries = maxRetries
	ps.retryDelay = retryDelay
}

// TradeRecord represents a trade in the database
type TradeRecord struct {
	TradeID     uuid.UUID
	BuyOrderID  uuid.UUID
	SellOrderID uuid.UUID
	Instrument  string
	Price       decimal.Decimal
	Quantity    decimal.Decimal
	Timestamp   time.Time
	CreatedAt   time.Time
}

// OrderUpdate represents order state to be updated
type OrderUpdate struct {
	OrderID        uuid.UUID
	FilledQuantity decimal.Decimal
	Status         models.OrderStatus
	UpdatedAt      time.Time
}

// PersistTradeWithOrders atomically persists a trade and updates both orders
// This is the main function that ensures consistency between trades and orders
func (ps *PostgresStore) PersistTradeWithOrders(ctx context.Context, trade *TradeRecord, buyOrderUpdate, sellOrderUpdate *OrderUpdate) error {
	return ps.executeWithRetry(ctx, func(ctx context.Context) error {
		return ps.persistTradeWithOrdersTx(ctx, trade, buyOrderUpdate, sellOrderUpdate)
	})
}

// PersistTradeOnly persists just the trade without updating orders
// Useful when orders are managed in-memory only
func (ps *PostgresStore) PersistTradeOnly(ctx context.Context, trade *TradeRecord) error {
	err := ps.executeWithRetry(ctx, func(ctx context.Context) error {
		tx, err := ps.db.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelReadCommitted,
		})
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() {
			_ = tx.Rollback() // Ignore error; will fail if transaction already committed
		}()

		err = ps.insertTrade(ctx, tx, trade)
		if err != nil {
			return fmt.Errorf("failed to insert trade: %w", err)
		}

		return tx.Commit()
	})
	if err != nil {
		logging.LogDBError("persist_trade_only", "trades", err, map[string]interface{}{
			"trade_id":   trade.TradeID,
			"instrument": trade.Instrument,
		})
		return err
	}
	logging.LogDBSuccess("persist_trade_only", "trades", 1, map[string]interface{}{
		"trade_id": trade.TradeID,
	})
	return nil
}

func (ps *PostgresStore) persistTradeWithOrdersTx(ctx context.Context, trade *TradeRecord, buyOrderUpdate, sellOrderUpdate *OrderUpdate) error {
	tx, err := ps.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback() // Ignore error; will fail if transaction already committed
	}()

	err = ps.insertTrade(ctx, tx, trade)
	if err != nil {
		logging.LogDBError("insert_trade", "trades", err, map[string]interface{}{
			"trade_id":   trade.TradeID,
			"instrument": trade.Instrument,
		})
		return fmt.Errorf("failed to insert trade: %w", err)
	}

	err = ps.updateOrder(ctx, tx, buyOrderUpdate)
	if err != nil {
		logging.LogDBError("update_order", "orders", err, map[string]interface{}{
			"order_id":   buyOrderUpdate.OrderID,
			"order_side": "buy",
		})
		return fmt.Errorf("failed to update buy order: %w", err)
	}

	err = ps.updateOrder(ctx, tx, sellOrderUpdate)
	if err != nil {
		logging.LogDBError("update_order", "orders", err, map[string]interface{}{
			"order_id":   sellOrderUpdate.OrderID,
			"order_side": "sell",
		})
		return fmt.Errorf("failed to update sell order: %w", err)
	}

	if err := tx.Commit(); err != nil {
		logging.LogDBError("commit_transaction", "trades,orders", err, map[string]interface{}{
			"trade_id": trade.TradeID,
		})
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// insertTrade inserts a trade with idempotency (ON CONFLICT DO NOTHING)
func (ps *PostgresStore) insertTrade(ctx context.Context, tx *sql.Tx, trade *TradeRecord) error {
	query := `
		INSERT INTO trades (
			trade_id, buy_order_id, sell_order_id, instrument,
			price, quantity, timestamp, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (trade_id) DO NOTHING
	`

	result, err := tx.ExecContext(ctx, query,
		trade.TradeID,
		trade.BuyOrderID,
		trade.SellOrderID,
		trade.Instrument,
		trade.Price.String(),
		trade.Quantity.String(),
		trade.Timestamp,
		trade.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to execute insert trade: %w", err)
	}

	// Check if row was inserted (for idempotency verification)
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// If 0 rows affected, trade already exists (idempotent operation)
	// This is not an error - it means the trade was already persisted
	if rowsAffected == 0 {
		// Log or handle duplicate trade ID (already persisted)
		return nil
	}

	return nil
}

// updateOrder updates an order's filled quantity and status
func (ps *PostgresStore) updateOrder(ctx context.Context, tx *sql.Tx, update *OrderUpdate) error {
	query := `
		UPDATE orders
		SET filled_quantity = $2,
		    status = $3,
		    updated_at = $4
		WHERE order_id = $1
	`

	result, err := tx.ExecContext(ctx, query,
		update.OrderID,
		update.FilledQuantity.String(),
		update.Status,
		update.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("order not found: %s", update.OrderID)
	}

	return nil
}

// executeWithRetry executes a function with retry logic for transient errors
func (ps *PostgresStore) executeWithRetry(ctx context.Context, fn func(context.Context) error) error {
	var lastErr error

	for attempt := 0; attempt <= ps.maxRetries; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if error is retryable
		if !ps.isRetryableError(err) {
			return err // Non-retryable error, fail immediately
		}

		// Don't sleep on last attempt
		if attempt < ps.maxRetries {
			// Exponential backoff
			delay := ps.retryDelay * time.Duration(1<<uint(attempt))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				// Continue to next attempt
			}
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// isRetryableError determines if an error is transient and should be retried
func (ps *PostgresStore) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for PostgreSQL-specific errors
	if pqErr, ok := err.(*pq.Error); ok {
		switch pqErr.Code {
		case "40001": // serialization_failure
			return true
		case "40P01": // deadlock_detected
			return true
		case "08000", "08003", "08006": // connection_exception, connection_does_not_exist, connection_failure
			return true
		case "57P03": // cannot_connect_now
			return true
		}
	}

	// Check for wrapped errors
	return errors.Is(err, ErrDeadlock) ||
		errors.Is(err, ErrSerializationFailure) ||
		errors.Is(err, ErrConnectionFailure)
}

// PersistMultipleTrades persists multiple trades and order updates in a single transaction
func (ps *PostgresStore) PersistMultipleTrades(ctx context.Context, trades []*TradeRecord, orderUpdates []*OrderUpdate) error {
	return ps.executeWithRetry(ctx, func(ctx context.Context) error {
		return ps.persistMultipleTradesTx(ctx, trades, orderUpdates)
	})
}

// persistMultipleTradesTx persists multiple trades in a single transaction
func (ps *PostgresStore) persistMultipleTradesTx(ctx context.Context, trades []*TradeRecord, orderUpdates []*OrderUpdate) error {
	tx, err := ps.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback() // Ignore error; will fail if transaction already committed
	}()

	// Insert all trades
	for _, trade := range trades {
		if err := ps.insertTrade(ctx, tx, trade); err != nil {
			return fmt.Errorf("failed to insert trade %s: %w", trade.TradeID, err)
		}
	}

	// Update all orders (de-duplicate by order ID)
	orderUpdateMap := make(map[uuid.UUID]*OrderUpdate)
	for _, update := range orderUpdates {
		orderUpdateMap[update.OrderID] = update
	}

	for _, update := range orderUpdateMap {
		if err := ps.updateOrder(ctx, tx, update); err != nil {
			return fmt.Errorf("failed to update order %s: %w", update.OrderID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetTrade retrieves a trade by ID
func (ps *PostgresStore) GetTrade(ctx context.Context, tradeID uuid.UUID) (*TradeRecord, error) {
	query := `
		SELECT trade_id, buy_order_id, sell_order_id, instrument,
		       price, quantity, timestamp, created_at
		FROM trades
		WHERE trade_id = $1
	`

	var trade TradeRecord
	var priceStr, quantityStr string

	err := ps.db.QueryRowContext(ctx, query, tradeID).Scan(
		&trade.TradeID,
		&trade.BuyOrderID,
		&trade.SellOrderID,
		&trade.Instrument,
		&priceStr,
		&quantityStr,
		&trade.Timestamp,
		&trade.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("trade not found: %s", tradeID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get trade: %w", err)
	}

	trade.Price, err = decimal.NewFromString(priceStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse price: %w", err)
	}

	trade.Quantity, err = decimal.NewFromString(quantityStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse quantity: %w", err)
	}

	return &trade, nil
}

// GetOrder retrieves an order by ID
func (ps *PostgresStore) GetOrder(ctx context.Context, orderID uuid.UUID) (*models.Order, error) {
	query := `
		SELECT order_id, client_id, instrument, side, type,
		       price, quantity, filled_quantity, status, timestamp, updated_at
		FROM orders
		WHERE order_id = $1
	`

	var order models.Order
	var priceStr, quantityStr, filledQtyStr string
	var sideStr, typeStr, statusStr string
	var updatedAt sql.NullTime

	err := ps.db.QueryRowContext(ctx, query, orderID).Scan(
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
		&updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get order: %w", err)
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

// InsertOrder inserts a new order
func (ps *PostgresStore) InsertOrder(ctx context.Context, order *models.Order) error {
	query := `
		INSERT INTO orders (
			order_id, client_id, instrument, side, type,
			price, quantity, filled_quantity, status, timestamp, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (order_id) DO NOTHING
	`

	_, err := ps.db.ExecContext(ctx, query,
		order.ID,
		order.ClientID,
		order.Instrument,
		order.Side,
		order.Type,
		order.Price.String(),
		order.Quantity.String(),
		order.FilledQuantity.String(),
		order.Status,
		order.CreatedAt,
		order.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to insert order: %w", err)
	}

	return nil
}

// GetTradesByInstrument retrieves all trades for a given instrument
func (ps *PostgresStore) GetTradesByInstrument(ctx context.Context, instrument string, limit int) ([]*TradeRecord, error) {
	query := `
		SELECT trade_id, buy_order_id, sell_order_id, instrument,
		       price, quantity, timestamp, created_at
		FROM trades
		WHERE instrument = $1
		ORDER BY timestamp DESC
		LIMIT $2
	`

	rows, err := ps.db.QueryContext(ctx, query, instrument, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query trades: %w", err)
	}
	defer func() {
		_ = rows.Close() // Ignore error on defer close
	}()

	trades := make([]*TradeRecord, 0)

	for rows.Next() {
		var trade TradeRecord
		var priceStr, quantityStr string

		err := rows.Scan(
			&trade.TradeID,
			&trade.BuyOrderID,
			&trade.SellOrderID,
			&trade.Instrument,
			&priceStr,
			&quantityStr,
			&trade.Timestamp,
			&trade.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan trade: %w", err)
		}

		trade.Price, err = decimal.NewFromString(priceStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse price: %w", err)
		}

		trade.Quantity, err = decimal.NewFromString(quantityStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse quantity: %w", err)
		}

		trades = append(trades, &trade)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating trades: %w", err)
	}

	return trades, nil
}

// GetOrdersByClientID retrieves all orders for a given client
func (ps *PostgresStore) GetOrdersByClientID(ctx context.Context, clientID string, limit int) ([]*models.Order, error) {
	query := `
		SELECT order_id, client_id, instrument, side, type,
		       price, quantity, filled_quantity, status, timestamp, updated_at
		FROM orders
		WHERE client_id = $1
		ORDER BY timestamp DESC
		LIMIT $2
	`

	rows, err := ps.db.QueryContext(ctx, query, clientID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query orders: %w", err)
	}
	defer func() {
		_ = rows.Close() // Ignore error on defer close
	}()

	orders := make([]*models.Order, 0)

	for rows.Next() {
		var order models.Order
		var priceStr, quantityStr, filledQtyStr string
		var sideStr, typeStr, statusStr string
		var updatedAt sql.NullTime

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
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}

		// Parse decimal fields
		order.Price, _ = decimal.NewFromString(priceStr)
		order.Quantity, _ = decimal.NewFromString(quantityStr)
		order.FilledQuantity, _ = decimal.NewFromString(filledQtyStr)

		// Parse enum fields
		order.Side = models.OrderSide(sideStr)
		order.Type = models.OrderType(typeStr)
		order.Status = models.OrderStatus(statusStr)

		orders = append(orders, &order)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating orders: %w", err)
	}

	return orders, nil
}
