-- Database Optimization Script for Trading Engine
-- Run this after initial setup to optimize performance

-- ============================================
-- 1. Enable Query Statistics
-- ============================================

-- Install pg_stat_statements extension
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Reset statistics
SELECT pg_stat_statements_reset();

-- Enable slow query logging (queries > 100ms)
ALTER SYSTEM SET log_min_duration_statement = 100;

-- Enable auto_explain for execution plans
ALTER SYSTEM SET auto_explain.log_min_duration = 100;
ALTER SYSTEM SET auto_explain.log_analyze = true;
ALTER SYSTEM SET auto_explain.log_buffers = true;
ALTER SYSTEM SET auto_explain.log_timing = true;

-- Reload configuration
SELECT pg_reload_conf();

\echo 'Query statistics and logging enabled'

-- ============================================
-- 2. Create Essential Indexes
-- ============================================

\echo ''
\echo '>Creating indexes for orders table...'

-- Instrument + Status (most common query)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_instrument_status 
ON orders(instrument, status) 
WHERE status IN ('OPEN', 'PARTIALLY_FILLED');

\echo '   ✓ idx_orders_instrument_status'

-- Created At (time-series queries)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_created_at 
ON orders(created_at DESC);

\echo '   ✓ idx_orders_created_at'

-- Client ID (user order lookup)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_client_id 
ON orders(client_id);

\echo '   ✓ idx_orders_client_id'

-- Orderbook reconstruction (bids)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_orderbook_bids
ON orders(instrument, side, price DESC, created_at)
WHERE status IN ('OPEN', 'PARTIALLY_FILLED') AND side = 'BUY';

\echo '   ✓ idx_orders_orderbook_bids'

-- Orderbook reconstruction (asks)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_orderbook_asks
ON orders(instrument, side, price ASC, created_at)
WHERE status IN ('OPEN', 'PARTIALLY_FILLED') AND side = 'SELL';

\echo '   ✓ idx_orders_orderbook_asks'

-- ============================================
\echo ''
\echo '>Creating indexes for trades table...'

-- Instrument + Timestamp (trade history)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_trades_instrument_timestamp 
ON trades(instrument, timestamp DESC);

\echo '   ✓ idx_trades_instrument_timestamp'

-- Buyer Order ID (find trades for order)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_trades_buyer_order 
ON trades(buyer_order_id);

\echo '   ✓ idx_trades_buyer_order'

-- Seller Order ID (find trades for order)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_trades_seller_order 
ON trades(seller_order_id);

\echo '   ✓ idx_trades_seller_order'

-- Price + Timestamp (OHLCV candlesticks)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_trades_instrument_price_timestamp 
ON trades(instrument, price, timestamp DESC);

\echo '   ✓ idx_trades_instrument_price_timestamp'

-- ============================================
\echo ''
\echo '>Creating indexes for snapshots table...'

-- Instrument + Created At (latest snapshot)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_snapshots_instrument_created 
ON orderbook_snapshots(instrument, created_at DESC);

\echo '   ✓ idx_snapshots_instrument_created'

-- Snapshot Type + Created At (cleanup)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_snapshots_type_created 
ON orderbook_snapshots(snapshot_type, created_at);

\echo '   ✓ idx_snapshots_type_created'

-- ============================================
-- 3. Update Table Statistics
-- ============================================

\echo ''
\echo '📈 Updating table statistics...'

ANALYZE orders;
\echo '   ✓ Analyzed orders'

ANALYZE trades;
\echo '   ✓ Analyzed trades'

ANALYZE orderbook_snapshots;
\echo '   ✓ Analyzed orderbook_snapshots'

-- ============================================
-- 4. Configure Autovacuum
-- ============================================

\echo ''
\echo '🧹 Configuring autovacuum...'

-- More aggressive autovacuum for high-write tables
ALTER TABLE orders SET (
    autovacuum_enabled = true,
    autovacuum_vacuum_scale_factor = 0.1,
    autovacuum_analyze_scale_factor = 0.05
);

ALTER TABLE trades SET (
    autovacuum_enabled = true,
    autovacuum_vacuum_scale_factor = 0.1,
    autovacuum_analyze_scale_factor = 0.05
);

\echo '   ✓ Autovacuum configured for orders and trades'

-- ============================================
-- 5. Performance Tuning
-- ============================================

\echo ''
\echo '⚙️  Applying performance tuning...'

-- Increase shared_buffers (if not already set)
-- Note: This requires restart, so just recommend
\echo '   ℹ️  Recommendation: Set shared_buffers = 256MB in postgresql.conf'
\echo '   ℹ️  Recommendation: Set effective_cache_size = 1GB in postgresql.conf'
\echo '   ℹ️  Recommendation: Set work_mem = 16MB in postgresql.conf'

-- ============================================
-- 6. Create Useful Views for Monitoring
-- ============================================

\echo ''
\echo '👁️  Creating monitoring views...'

-- Top slow queries
CREATE OR REPLACE VIEW v_slow_queries AS
SELECT 
    calls,
    total_exec_time,
    mean_exec_time,
    max_exec_time,
    stddev_exec_time,
    substring(query, 1, 80) AS query_snippet
FROM pg_stat_statements
WHERE mean_exec_time > 10  -- Queries averaging > 10ms
ORDER BY mean_exec_time DESC
LIMIT 20;

\echo '   ✓ Created v_slow_queries'

-- Index usage statistics
CREATE OR REPLACE VIEW v_index_usage AS
SELECT
    schemaname,
    tablename,
    indexname,
    idx_scan AS scans,
    idx_tup_read AS tuples_read,
    idx_tup_fetch AS tuples_fetched,
    pg_size_pretty(pg_relation_size(indexrelid)) AS size,
    CASE WHEN idx_scan = 0 THEN ' UNUSED' ELSE '✓ Used' END AS status
FROM pg_stat_user_indexes
WHERE schemaname = 'public'
ORDER BY idx_scan DESC;

\echo '   ✓ Created v_index_usage'

-- Table statistics
CREATE OR REPLACE VIEW v_table_stats AS
SELECT 
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS total_size,
    pg_size_pretty(pg_relation_size(schemaname||'.'||tablename)) AS table_size,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename) - pg_relation_size(schemaname||'.'||tablename)) AS index_size,
    n_live_tup AS live_rows,
    n_dead_tup AS dead_rows,
    CASE 
        WHEN n_live_tup > 0 THEN round(100.0 * n_dead_tup / n_live_tup, 2)
        ELSE 0
    END AS dead_row_percent,
    last_vacuum,
    last_autovacuum,
    last_analyze,
    last_autoanalyze
FROM pg_stat_user_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;

\echo '   ✓ Created v_table_stats'

-- Cache hit ratio
CREATE OR REPLACE VIEW v_cache_hit_ratio AS
SELECT 
    sum(heap_blks_read) AS heap_read,
    sum(heap_blks_hit) AS heap_hit,
    CASE 
        WHEN (sum(heap_blks_hit) + sum(heap_blks_read)) > 0 
        THEN round(100.0 * sum(heap_blks_hit) / (sum(heap_blks_hit) + sum(heap_blks_read)), 2)
        ELSE 0
    END AS cache_hit_ratio_percent
FROM pg_statio_user_tables;

\echo '   ✓ Created v_cache_hit_ratio'

-- ============================================
-- 7. Verification Queries
-- ============================================

\echo ''
\echo '========================================'
\echo 'Optimization Complete!'
\echo '========================================'
\echo ''

-- Show index count
\echo 'Created indexes:'
SELECT 
    schemaname,
    tablename,
    COUNT(*) AS index_count
FROM pg_indexes
WHERE schemaname = 'public'
GROUP BY schemaname, tablename
ORDER BY tablename;

\echo ''
\echo 'Table sizes:'
SELECT * FROM v_table_stats;

\echo ''
\echo 'Cache hit ratio (should be > 99%):'
SELECT * FROM v_cache_hit_ratio;

\echo ''
\echo '========================================'
\echo 'Monitoring Commands'
\echo '========================================'
\echo ''
\echo '1. View slow queries:'
\echo '   SELECT * FROM v_slow_queries;'
\echo ''
\echo '2. Check index usage:'
\echo '   SELECT * FROM v_index_usage WHERE status LIKE ''%UNUSED%'';'
\echo ''
\echo '3. Check table stats:'
\echo '   SELECT * FROM v_table_stats;'
\echo ''
\echo '4. Check cache hit ratio:'
\echo '   SELECT * FROM v_cache_hit_ratio;'
\echo ''
\echo '5. Find queries needing optimization:'
\echo '   SELECT query, calls, mean_exec_time FROM pg_stat_statements'
\echo '   WHERE mean_exec_time > 10 ORDER BY mean_exec_time DESC LIMIT 10;'
\echo ''
\echo '6. Analyze query plan:'
\echo '   EXPLAIN (ANALYZE, BUFFERS) SELECT * FROM orders WHERE instrument = ''BTC-USD'';'
\echo ''
\echo '========================================'
\echo 'Next Steps'
\echo '========================================'
\echo ''
\echo '1. Run load test to generate query patterns'
\echo '2. Check v_slow_queries for optimization opportunities'
\echo '3. Verify cache_hit_ratio > 99%'
\echo '4. Check for unused indexes in v_index_usage'
\echo '5. Monitor dead_row_percent in v_table_stats'
\echo ''
\echo 'Database optimization complete!'
