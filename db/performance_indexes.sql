-- Performance Optimization Indexes for Trading Engine
-- PostgreSQL 14+

-- ============================================================================
-- TRADES TABLE INDEXES (for GET /trades endpoint)
-- ============================================================================

-- Primary index for trades pagination with stable sort
-- Supports: ORDER BY timestamp DESC, trade_id DESC
-- This composite index ensures consistent pagination
CREATE INDEX IF NOT EXISTS idx_trades_timestamp_id 
ON trades(instrument, timestamp DESC, trade_id DESC);

-- Index for filtering by timestamp range (for time-based queries)
CREATE INDEX IF NOT EXISTS idx_trades_timestamp_range 
ON trades(timestamp DESC) 
WHERE timestamp > NOW() - INTERVAL '24 hours';

-- Covering index for common trade queries (includes all columns in index)
-- This allows index-only scans without touching the table
CREATE INDEX IF NOT EXISTS idx_trades_covering 
ON trades(instrument, timestamp DESC, trade_id DESC) 
INCLUDE (buy_order_id, sell_order_id, price, quantity);

-- ============================================================================
-- ORDERS TABLE INDEXES (for GET /orders/{id} endpoint)
-- ============================================================================

-- Primary key index (already created by PRIMARY KEY constraint)
-- But we ensure it exists explicitly
-- CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_pk ON orders(order_id);

-- Index for order status queries (hot orders = open/partially_filled)
CREATE INDEX IF NOT EXISTS idx_orders_hot_status 
ON orders(status, updated_at DESC) 
WHERE status IN ('open', 'partially_filled');

-- Covering index for order lookups (avoids table access)
CREATE INDEX IF NOT EXISTS idx_orders_lookup_covering 
ON orders(order_id) 
INCLUDE (client_id, instrument, side, type, price, quantity, filled_quantity, status, timestamp);

-- Index for client's recent orders
CREATE INDEX IF NOT EXISTS idx_orders_client_recent 
ON orders(client_id, timestamp DESC);

-- Index for instrument-specific order queries
CREATE INDEX IF NOT EXISTS idx_orders_instrument_status 
ON orders(instrument, status, timestamp DESC);

-- ============================================================================
-- PARTITIONING RECOMMENDATIONS (for high-volume trading)
-- ============================================================================

-- For very high trade volumes, consider partitioning trades table by date
-- This improves query performance and allows easy archival

/*
-- Example: Partition trades by month
CREATE TABLE trades_partitioned (
    LIKE trades INCLUDING ALL
) PARTITION BY RANGE (timestamp);

-- Create partitions for each month
CREATE TABLE trades_2025_11 PARTITION OF trades_partitioned
    FOR VALUES FROM ('2025-11-01') TO ('2025-12-01');

CREATE TABLE trades_2025_12 PARTITION OF trades_partitioned
    FOR VALUES FROM ('2025-12-01') TO ('2026-01-01');

-- Auto-create partitions using pg_partman extension
-- Or create them via cron job/scheduled task
*/

-- ============================================================================
-- QUERY PERFORMANCE ANALYSIS
-- ============================================================================

-- Enable query timing for performance analysis
-- Run this before testing queries:
-- \timing on

-- Test query performance for GET /trades
EXPLAIN (ANALYZE, BUFFERS) 
SELECT trade_id, buy_order_id, sell_order_id, instrument, price, quantity, timestamp
FROM trades
WHERE instrument = 'BTC-USD'
ORDER BY timestamp DESC, trade_id DESC
LIMIT 50 OFFSET 0;

-- Test query performance for GET /orders/{id}
EXPLAIN (ANALYZE, BUFFERS)
SELECT order_id, client_id, instrument, side, type, price, quantity, filled_quantity, status, timestamp
FROM orders
WHERE order_id = '00000000-0000-0000-0000-000000000000'::uuid;

-- ============================================================================
-- CACHING STRATEGY FOR HOT ORDERS
-- ============================================================================

-- Materialized view for frequently accessed "hot" orders (open/partially filled)
CREATE MATERIALIZED VIEW IF NOT EXISTS hot_orders AS
SELECT 
    order_id,
    client_id,
    instrument,
    side,
    type,
    price,
    quantity,
    filled_quantity,
    status,
    timestamp,
    updated_at
FROM orders
WHERE status IN ('open', 'partially_filled')
ORDER BY updated_at DESC;

-- Index on materialized view
CREATE UNIQUE INDEX IF NOT EXISTS idx_hot_orders_id ON hot_orders(order_id);
CREATE INDEX IF NOT EXISTS idx_hot_orders_updated ON hot_orders(updated_at DESC);

-- Refresh materialized view (should be done periodically, e.g., every 10 seconds)
-- REFRESH MATERIALIZED VIEW CONCURRENTLY hot_orders;

-- For application-level caching, consider:
-- 1. Redis cache for hot orders (TTL: 10 seconds)
-- 2. In-memory LRU cache for recently accessed orders
-- 3. Edge caching for public trade data

-- ============================================================================
-- CONNECTION POOL SETTINGS (for main.go)
-- ============================================================================

-- Recommended PostgreSQL connection pool settings:
-- db.SetMaxOpenConns(50)       // Max connections (based on expected load)
-- db.SetMaxIdleConns(10)       // Idle connections to keep alive
-- db.SetConnMaxLifetime(5 * time.Minute)  // Recycle connections
-- db.SetConnMaxIdleTime(30 * time.Second) // Close idle connections

-- ============================================================================
-- POSTGRESQL SERVER TUNING
-- ============================================================================

-- Add to postgresql.conf for better performance:
/*
# Memory settings
shared_buffers = 4GB              # 25% of system RAM
effective_cache_size = 12GB       # 75% of system RAM
work_mem = 50MB                   # Per-connection memory for sorts
maintenance_work_mem = 1GB        # For CREATE INDEX, VACUUM

# Query planner settings
random_page_cost = 1.1            # For SSD storage
effective_io_concurrency = 200    # For SSD storage

# Write-ahead log settings
wal_buffers = 16MB
checkpoint_completion_target = 0.9
max_wal_size = 4GB
min_wal_size = 1GB

# Connection settings
max_connections = 200
*/

-- ============================================================================
-- MONITORING QUERIES
-- ============================================================================

-- Check index usage statistics
SELECT 
    schemaname,
    tablename,
    indexname,
    idx_scan as index_scans,
    idx_tup_read as tuples_read,
    idx_tup_fetch as tuples_fetched
FROM pg_stat_user_indexes
WHERE schemaname = 'public'
ORDER BY idx_scan DESC;

-- Check table sizes
SELECT 
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;

-- Check slow queries (requires pg_stat_statements extension)
/*
SELECT 
    query,
    calls,
    total_exec_time,
    mean_exec_time,
    max_exec_time
FROM pg_stat_statements
ORDER BY mean_exec_time DESC
LIMIT 10;
*/

-- ============================================================================
-- VACUUM AND MAINTENANCE
-- ============================================================================

-- Enable auto-vacuum for high-churn tables
ALTER TABLE trades SET (autovacuum_vacuum_scale_factor = 0.01);
ALTER TABLE orders SET (autovacuum_vacuum_scale_factor = 0.02);

-- Manual vacuum (run periodically during low-traffic hours)
-- VACUUM ANALYZE trades;
-- VACUUM ANALYZE orders;

-- Reindex for performance (if indexes become bloated)
-- REINDEX TABLE CONCURRENTLY trades;
-- REINDEX TABLE CONCURRENTLY orders;
