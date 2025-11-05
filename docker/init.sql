-- Trading Engine Database Schema

-- Orders table
CREATE TABLE IF NOT EXISTS orders (
    order_id VARCHAR(255) PRIMARY KEY,
    client_id VARCHAR(255) NOT NULL,
    instrument VARCHAR(50) NOT NULL,
    side VARCHAR(10) NOT NULL CHECK (side IN ('buy', 'sell')),
    type VARCHAR(10) NOT NULL CHECK (type IN ('limit', 'market')),
    price DECIMAL(20, 8),
    quantity DECIMAL(20, 8) NOT NULL,
    filled_quantity DECIMAL(20, 8) NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL CHECK (status IN ('open', 'partially_filled', 'filled', 'cancelled', 'rejected')),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Orders snapshot table (for recovery system)
-- This is a materialized view of current order state
-- Since orders are managed in-memory, this will typically be empty
-- but is required for the recovery system to function
CREATE TABLE IF NOT EXISTS orders_snapshot (
    order_id VARCHAR(255) PRIMARY KEY,
    client_id VARCHAR(255) NOT NULL,
    instrument VARCHAR(50) NOT NULL,
    side VARCHAR(10) NOT NULL CHECK (side IN ('buy', 'sell')),
    type VARCHAR(10) NOT NULL CHECK (type IN ('limit', 'market')),
    price DECIMAL(20, 8),
    quantity DECIMAL(20, 8) NOT NULL,
    filled_quantity DECIMAL(20, 8) NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL CHECK (status IN ('open', 'partially_filled', 'filled', 'cancelled', 'rejected')),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Trades table
CREATE TABLE IF NOT EXISTS trades (
    trade_id VARCHAR(255) PRIMARY KEY,
    buy_order_id VARCHAR(255) NOT NULL,
    sell_order_id VARCHAR(255) NOT NULL,
    instrument VARCHAR(50) NOT NULL,
    price DECIMAL(20, 8) NOT NULL,
    quantity DECIMAL(20, 8) NOT NULL,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
    -- Note: No foreign key constraints since orders are managed in-memory only
);

-- Order book snapshots table
CREATE TABLE IF NOT EXISTS orderbook_snapshots (
    snapshot_id SERIAL PRIMARY KEY,
    instrument VARCHAR(50) NOT NULL,
    snapshot_data JSONB NOT NULL,
    bid_count INTEGER NOT NULL DEFAULT 0,
    ask_count INTEGER NOT NULL DEFAULT 0,
    best_bid_price DECIMAL(20, 8),
    best_ask_price DECIMAL(20, 8),
    spread DECIMAL(20, 8),
    total_bid_volume DECIMAL(20, 8),
    total_ask_volume DECIMAL(20, 8),
    snapshot_timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
    snapshot_type VARCHAR(20) NOT NULL DEFAULT 'periodic',
    triggered_by VARCHAR(50) NOT NULL DEFAULT 'system',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Idempotency keys table
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key VARCHAR(255) PRIMARY KEY,
    order_id VARCHAR(255) NOT NULL,
    response_data JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL
);

-- Indexes for performance
CREATE INDEX idx_orders_client_id ON orders(client_id);
CREATE INDEX idx_orders_instrument ON orders(instrument);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_created_at ON orders(created_at DESC);

CREATE INDEX idx_orders_snapshot_instrument ON orders_snapshot(instrument);
CREATE INDEX idx_orders_snapshot_status ON orders_snapshot(status);
CREATE INDEX idx_orders_snapshot_created_at ON orders_snapshot(created_at DESC);

CREATE INDEX idx_trades_instrument ON trades(instrument);
CREATE INDEX idx_trades_timestamp ON trades(timestamp DESC);
CREATE INDEX idx_trades_buy_order ON trades(buy_order_id);
CREATE INDEX idx_trades_sell_order ON trades(sell_order_id);

CREATE INDEX idx_snapshots_instrument ON orderbook_snapshots(instrument);
CREATE INDEX idx_snapshots_created_at ON orderbook_snapshots(created_at DESC);

CREATE INDEX idx_idempotency_expires ON idempotency_keys(expires_at);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger to auto-update updated_at
CREATE TRIGGER update_orders_updated_at BEFORE UPDATE ON orders
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Cleanup expired idempotency keys (run periodically)
CREATE OR REPLACE FUNCTION cleanup_expired_idempotency_keys()
RETURNS void AS $$
BEGIN
    DELETE FROM idempotency_keys WHERE expires_at < NOW();
END;
$$ LANGUAGE plpgsql;

-- Function to get latest snapshot for an instrument
CREATE OR REPLACE FUNCTION get_latest_snapshot(p_instrument VARCHAR)
RETURNS TABLE(
    snapshot_id INTEGER,
    instrument VARCHAR(50),
    snapshot_data JSONB,
    bid_count INTEGER,
    ask_count INTEGER,
    best_bid_price DECIMAL(20, 8),
    best_ask_price DECIMAL(20, 8),
    spread DECIMAL(20, 8),
    total_bid_volume DECIMAL(20, 8),
    total_ask_volume DECIMAL(20, 8),
    snapshot_timestamp TIMESTAMP,
    snapshot_type VARCHAR(20),
    triggered_by VARCHAR(50),
    created_at TIMESTAMP
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        os.snapshot_id,
        os.instrument,
        os.snapshot_data,
        os.bid_count,
        os.ask_count,
        os.best_bid_price,
        os.best_ask_price,
        os.spread,
        os.total_bid_volume,
        os.total_ask_volume,
        os.snapshot_timestamp,
        os.snapshot_type,
        os.triggered_by,
        os.created_at
    FROM orderbook_snapshots os
    WHERE os.instrument = p_instrument
    ORDER BY os.created_at DESC
    LIMIT 1;
END;
$$ LANGUAGE plpgsql;

-- Grant permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO trader;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO trader;
