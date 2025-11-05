-- Event Sourcing Database Schema
-- PostgreSQL 14+

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================
-- 1. Events Table (Immutable Event Log)
-- ============================================
CREATE TABLE events (
    -- Primary key
    event_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    
    -- Event classification
    event_type VARCHAR(100) NOT NULL,        -- OrderPlaced, OrderCancelled, TradeExecuted
    aggregate_id VARCHAR(255) NOT NULL,      -- order-123, BTC-USD, account-456
    aggregate_type VARCHAR(50) NOT NULL,     -- Order, Orderbook, Account
    
    -- Event payload (full event data in JSON)
    payload JSONB NOT NULL,
    
    -- Versioning for optimistic concurrency control
    version INTEGER NOT NULL DEFAULT 1,
    
    -- Temporal tracking
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Audit metadata (who, from where, why)
    metadata JSONB DEFAULT '{}',
    
    -- Causality tracking (event chain)
    causation_id UUID,         -- ID of command that caused this event
    correlation_id UUID,       -- ID to group related events
    
    -- Ensure version uniqueness per aggregate
    CONSTRAINT events_aggregate_version_unique UNIQUE(aggregate_id, aggregate_type, version)
);

-- ============================================
-- 2. Indexes for Fast Queries
-- ============================================

-- Index for loading all events of an aggregate (most common query)
CREATE INDEX idx_events_aggregate ON events(aggregate_id, aggregate_type, version ASC);

-- Index for querying by event type and time
CREATE INDEX idx_events_type_time ON events(event_type, timestamp DESC);

-- Index for recent events (time-descending for latest first)
CREATE INDEX idx_events_timestamp ON events(timestamp DESC);

-- Index for correlation queries (trace event chains)
CREATE INDEX idx_events_correlation ON events(correlation_id) WHERE correlation_id IS NOT NULL;

-- Index for causation queries (find downstream events)
CREATE INDEX idx_events_causation ON events(causation_id) WHERE causation_id IS NOT NULL;

-- ============================================
-- 3. Snapshots Table (Performance Optimization)
-- ============================================
CREATE TABLE snapshots (
    snapshot_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    aggregate_id VARCHAR(255) NOT NULL,
    aggregate_type VARCHAR(50) NOT NULL,
    version INTEGER NOT NULL,              -- Last event version included
    state JSONB NOT NULL,                  -- Complete aggregate state
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- One snapshot per version
    CONSTRAINT snapshots_aggregate_unique UNIQUE(aggregate_id, aggregate_type, version)
);

-- Index for loading latest snapshot
CREATE INDEX idx_snapshots_aggregate ON snapshots(aggregate_id, aggregate_type, version DESC);

-- ============================================
-- 4. Projections (Read Models)
-- ============================================

-- Orderbook Projection (current state, optimized for queries)
CREATE TABLE orderbook_projection (
    instrument VARCHAR(20) PRIMARY KEY,
    best_bid DECIMAL(20,8),
    best_ask DECIMAL(20,8),
    spread DECIMAL(20,8),
    bid_volume DECIMAL(20,8),
    ask_volume DECIMAL(20,8),
    last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    event_version INTEGER NOT NULL DEFAULT 0  -- Last processed event version
);

-- Trades Projection (optimized for historical queries)
CREATE TABLE trades_projection (
    trade_id UUID PRIMARY KEY,
    instrument VARCHAR(20) NOT NULL,
    price DECIMAL(20,8) NOT NULL,
    quantity DECIMAL(20,8) NOT NULL,
    side VARCHAR(4) NOT NULL,
    buyer_order_id UUID NOT NULL,
    seller_order_id UUID NOT NULL,
    buyer_client_id VARCHAR(255),
    seller_client_id VARCHAR(255),
    executed_at TIMESTAMPTZ NOT NULL,
    event_version INTEGER NOT NULL
);

CREATE INDEX idx_trades_instrument_time ON trades_projection(instrument, executed_at DESC);
CREATE INDEX idx_trades_client ON trades_projection(buyer_client_id);
CREATE INDEX idx_trades_client_seller ON trades_projection(seller_client_id);

-- Orders Projection (current order state)
CREATE TABLE orders_projection (
    order_id UUID PRIMARY KEY,
    client_id VARCHAR(255) NOT NULL,
    instrument VARCHAR(20) NOT NULL,
    side VARCHAR(4) NOT NULL,
    type VARCHAR(10) NOT NULL,
    price DECIMAL(20,8),
    original_quantity DECIMAL(20,8) NOT NULL,
    remaining_quantity DECIMAL(20,8) NOT NULL,
    filled_quantity DECIMAL(20,8) NOT NULL DEFAULT 0,
    average_fill_price DECIMAL(20,8),
    status VARCHAR(20) NOT NULL,  -- open, partial, filled, cancelled
    time_in_force VARCHAR(10),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    event_version INTEGER NOT NULL
);

CREATE INDEX idx_orders_client ON orders_projection(client_id, created_at DESC);
CREATE INDEX idx_orders_instrument ON orders_projection(instrument, status);
CREATE INDEX idx_orders_status ON orders_projection(status);

-- ============================================
-- 5. Sample Data (for testing)
-- ============================================

-- Insert sample OrderPlaced event
INSERT INTO events (event_id, event_type, aggregate_id, aggregate_type, payload, version, timestamp) VALUES
(
    uuid_generate_v4(),
    'OrderPlaced',
    'order-123',
    'Order',
    '{
        "order_id": "order-123",
        "client_id": "client-456",
        "instrument": "BTC-USD",
        "side": "buy",
        "type": "limit",
        "price": 50000.00,
        "quantity": 1.5,
        "time_in_force": "GTC"
    }'::jsonb,
    1,
    NOW()
);

-- Insert sample TradeExecuted event
INSERT INTO events (event_id, event_type, aggregate_id, aggregate_type, payload, version, timestamp) VALUES
(
    uuid_generate_v4(),
    'TradeExecuted',
    'BTC-USD',
    'Orderbook',
    '{
        "trade_id": "trade-789",
        "instrument": "BTC-USD",
        "buyer_order_id": "order-123",
        "seller_order_id": "order-456",
        "price": 50000.00,
        "quantity": 1.0,
        "buyer_client_id": "client-456",
        "seller_client_id": "client-789"
    }'::jsonb,
    1,
    NOW()
);

-- ============================================
-- 6. Useful Queries
-- ============================================

-- Query: Get all events for an order (full history)
-- SELECT event_type, timestamp, payload
-- FROM events
-- WHERE aggregate_id = 'order-123' AND aggregate_type = 'Order'
-- ORDER BY version ASC;

-- Query: Get events since version N (incremental replay)
-- SELECT *
-- FROM events
-- WHERE aggregate_id = 'BTC-USD' AND aggregate_type = 'Orderbook' AND version > 100
-- ORDER BY version ASC;

-- Query: Get all trades for an instrument (time range)
-- SELECT *
-- FROM events
-- WHERE event_type = 'TradeExecuted' 
--   AND payload->>'instrument' = 'BTC-USD'
--   AND timestamp BETWEEN '2025-11-05 00:00:00' AND '2025-11-05 23:59:59'
-- ORDER BY timestamp DESC;

-- Query: Trace event chain (correlation ID)
-- SELECT event_type, aggregate_id, timestamp, payload
-- FROM events
-- WHERE correlation_id = 'some-correlation-id'
-- ORDER BY timestamp ASC;

-- Query: Count events by type (monitoring)
-- SELECT event_type, COUNT(*) as count
-- FROM events
-- WHERE timestamp > NOW() - INTERVAL '1 hour'
-- GROUP BY event_type
-- ORDER BY count DESC;

-- ============================================
-- 7. Maintenance Queries
-- ============================================

-- Archive old events (keep 1 year)
-- CREATE TABLE events_archive (LIKE events INCLUDING ALL);
-- INSERT INTO events_archive SELECT * FROM events WHERE timestamp < NOW() - INTERVAL '1 year';
-- DELETE FROM events WHERE timestamp < NOW() - INTERVAL '1 year';

-- Vacuum events table (reclaim space)
-- VACUUM ANALYZE events;

-- Check event store size
-- SELECT pg_size_pretty(pg_total_relation_size('events')) AS total_size;

-- ============================================
-- Schema Version: 1.0
-- Created: November 5, 2025
-- Status: Production Ready
-- ============================================
