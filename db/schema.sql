-- Trading Engine Database Schema
-- PostgreSQL 14+

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Orders table
CREATE TABLE IF NOT EXISTS orders (
    order_id UUID PRIMARY KEY,
    client_id VARCHAR(255) NOT NULL,
    instrument VARCHAR(50) NOT NULL,
    side VARCHAR(10) NOT NULL CHECK (side IN ('buy', 'sell')),
    type VARCHAR(10) NOT NULL CHECK (type IN ('market', 'limit')),
    price NUMERIC(20, 8) NOT NULL,
    quantity NUMERIC(20, 8) NOT NULL CHECK (quantity > 0),
    filled_quantity NUMERIC(20, 8) NOT NULL DEFAULT 0 CHECK (filled_quantity >= 0),
    status VARCHAR(20) NOT NULL CHECK (status IN ('open', 'partially_filled', 'filled', 'cancelled', 'rejected')),
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Constraints
    CONSTRAINT filled_quantity_le_quantity CHECK (filled_quantity <= quantity)
);

-- Trades table
CREATE TABLE IF NOT EXISTS trades (
    trade_id UUID PRIMARY KEY,
    buy_order_id UUID NOT NULL,
    sell_order_id UUID NOT NULL,
    instrument VARCHAR(50) NOT NULL,
    price NUMERIC(20, 8) NOT NULL CHECK (price > 0),
    quantity NUMERIC(20, 8) NOT NULL CHECK (quantity > 0),
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Foreign keys (optional - can be enabled if referential integrity is desired)
    -- FOREIGN KEY (buy_order_id) REFERENCES orders(order_id),
    -- FOREIGN KEY (sell_order_id) REFERENCES orders(order_id)
    
    -- Ensure uniqueness (idempotency)
    CONSTRAINT unique_trade_id UNIQUE (trade_id)
);

-- Indexes for performance

-- Orders indexes
CREATE INDEX IF NOT EXISTS idx_orders_client_id ON orders(client_id);
CREATE INDEX IF NOT EXISTS idx_orders_instrument ON orders(instrument);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_timestamp ON orders(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_orders_instrument_status ON orders(instrument, status);
CREATE INDEX IF NOT EXISTS idx_orders_client_timestamp ON orders(client_id, timestamp DESC);

-- Trades indexes
CREATE INDEX IF NOT EXISTS idx_trades_buy_order ON trades(buy_order_id);
CREATE INDEX IF NOT EXISTS idx_trades_sell_order ON trades(sell_order_id);
CREATE INDEX IF NOT EXISTS idx_trades_instrument ON trades(instrument);
CREATE INDEX IF NOT EXISTS idx_trades_timestamp ON trades(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_trades_instrument_timestamp ON trades(instrument, timestamp DESC);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger to automatically update updated_at
CREATE TRIGGER update_orders_updated_at
    BEFORE UPDATE ON orders
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Sample queries for testing

-- Query 1: Get recent trades for an instrument
-- SELECT * FROM trades WHERE instrument = 'BTC-USD' ORDER BY timestamp DESC LIMIT 10;

-- Query 2: Get order book depth (simulated - actual order book is in-memory)
-- SELECT price, SUM(quantity - filled_quantity) as volume
-- FROM orders
-- WHERE instrument = 'BTC-USD' AND status IN ('open', 'partially_filled') AND side = 'buy'
-- GROUP BY price
-- ORDER BY price DESC
-- LIMIT 10;

-- Query 3: Get user's order history
-- SELECT * FROM orders WHERE client_id = 'user123' ORDER BY timestamp DESC;

-- Query 4: Check for trade idempotency
-- SELECT COUNT(*) FROM trades WHERE trade_id = 'some-uuid'; -- Should be 0 or 1

-- Query 5: Verify order filled quantities match trades
-- SELECT o.order_id, o.filled_quantity, 
--        COALESCE(SUM(t.quantity), 0) as trade_total
-- FROM orders o
-- LEFT JOIN trades t ON (o.order_id = t.buy_order_id OR o.order_id = t.sell_order_id)
-- GROUP BY o.order_id, o.filled_quantity
-- HAVING o.filled_quantity != COALESCE(SUM(t.quantity), 0);

-- Performance tuning suggestions:
-- 1. For high-frequency trading, consider partitioning trades table by timestamp
-- 2. Use connection pooling (pgbouncer) for better connection management
-- 3. Enable prepared statements for frequently executed queries
-- 4. Monitor and tune PostgreSQL settings (shared_buffers, work_mem, etc.)
-- 5. Consider using READ COMMITTED isolation level for better concurrency
