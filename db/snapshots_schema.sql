-- ============================================================================
-- Orderbook Snapshots Schema
-- ============================================================================
-- Stores periodic snapshots of orderbook state for recovery and analytics

CREATE TABLE IF NOT EXISTS orderbook_snapshots (
    snapshot_id BIGSERIAL PRIMARY KEY,
    instrument VARCHAR(50) NOT NULL,
    snapshot_data JSONB NOT NULL,
    bid_count INTEGER NOT NULL DEFAULT 0,
    ask_count INTEGER NOT NULL DEFAULT 0,
    best_bid_price NUMERIC(20, 8),
    best_ask_price NUMERIC(20, 8),
    spread NUMERIC(20, 8),
    total_bid_volume NUMERIC(20, 8) NOT NULL DEFAULT 0,
    total_ask_volume NUMERIC(20, 8) NOT NULL DEFAULT 0,
    snapshot_timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Metadata
    snapshot_type VARCHAR(20) NOT NULL DEFAULT 'periodic',
    triggered_by VARCHAR(255),
    
    CHECK (snapshot_type IN ('periodic', 'on-demand', 'pre-shutdown', 'recovery'))
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_snapshots_instrument ON orderbook_snapshots(instrument);
CREATE INDEX IF NOT EXISTS idx_snapshots_timestamp ON orderbook_snapshots(snapshot_timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_snapshots_instrument_timestamp ON orderbook_snapshots(instrument, snapshot_timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_snapshots_type ON orderbook_snapshots(snapshot_type);

-- Function to get latest snapshot for an instrument
CREATE OR REPLACE FUNCTION get_latest_snapshot(p_instrument VARCHAR(50))
RETURNS TABLE (
    snapshot_id BIGINT,
    instrument VARCHAR(50),
    snapshot_data JSONB,
    bid_count INTEGER,
    ask_count INTEGER,
    best_bid_price NUMERIC(20, 8),
    best_ask_price NUMERIC(20, 8),
    spread NUMERIC(20, 8),
    snapshot_timestamp TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        s.snapshot_id,
        s.instrument,
        s.snapshot_data,
        s.bid_count,
        s.ask_count,
        s.best_bid_price,
        s.best_ask_price,
        s.spread,
        s.snapshot_timestamp,
        s.created_at
    FROM orderbook_snapshots s
    WHERE s.instrument = p_instrument
    ORDER BY s.snapshot_timestamp DESC
    LIMIT 1;
END;
$$ LANGUAGE plpgsql;

-- Function to get snapshots within time range
CREATE OR REPLACE FUNCTION get_snapshots_in_range(
    p_instrument VARCHAR(50),
    p_start_time TIMESTAMP WITH TIME ZONE,
    p_end_time TIMESTAMP WITH TIME ZONE
)
RETURNS TABLE (
    snapshot_id BIGINT,
    snapshot_timestamp TIMESTAMP WITH TIME ZONE,
    bid_count INTEGER,
    ask_count INTEGER,
    best_bid_price NUMERIC(20, 8),
    best_ask_price NUMERIC(20, 8),
    spread NUMERIC(20, 8)
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        s.snapshot_id,
        s.snapshot_timestamp,
        s.bid_count,
        s.ask_count,
        s.best_bid_price,
        s.best_ask_price,
        s.spread
    FROM orderbook_snapshots s
    WHERE s.instrument = p_instrument
      AND s.snapshot_timestamp >= p_start_time
      AND s.snapshot_timestamp <= p_end_time
    ORDER BY s.snapshot_timestamp ASC;
END;
$$ LANGUAGE plpgsql;

-- Function to cleanup old snapshots (keep last N per instrument)
CREATE OR REPLACE FUNCTION cleanup_old_snapshots(
    p_instrument VARCHAR(50),
    p_keep_count INTEGER DEFAULT 100
)
RETURNS INTEGER AS $$
DECLARE
    v_deleted_count INTEGER;
BEGIN
    WITH snapshots_to_delete AS (
        SELECT snapshot_id
        FROM orderbook_snapshots
        WHERE instrument = p_instrument
        ORDER BY snapshot_timestamp DESC
        OFFSET p_keep_count
    )
    DELETE FROM orderbook_snapshots
    WHERE snapshot_id IN (SELECT snapshot_id FROM snapshots_to_delete);
    
    GET DIAGNOSTICS v_deleted_count = ROW_COUNT;
    RETURN v_deleted_count;
END;
$$ LANGUAGE plpgsql;

-- Automatic cleanup trigger (optional)
CREATE OR REPLACE FUNCTION auto_cleanup_snapshots()
RETURNS TRIGGER AS $$
DECLARE
    v_count INTEGER;
BEGIN
    -- Check count for this instrument
    SELECT COUNT(*) INTO v_count
    FROM orderbook_snapshots
    WHERE instrument = NEW.instrument;
    
    -- If more than 1000 snapshots, cleanup
    IF v_count > 1000 THEN
        PERFORM cleanup_old_snapshots(NEW.instrument, 500);
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_auto_cleanup_snapshots
    AFTER INSERT ON orderbook_snapshots
    FOR EACH ROW
    EXECUTE FUNCTION auto_cleanup_snapshots();

-- Comments
COMMENT ON TABLE orderbook_snapshots IS 'Periodic snapshots of orderbook state for recovery and analytics';
COMMENT ON COLUMN orderbook_snapshots.snapshot_data IS 'JSONB containing serialized orderbook levels';
COMMENT ON COLUMN orderbook_snapshots.snapshot_type IS 'Type: periodic, on-demand, pre-shutdown, recovery';
COMMENT ON FUNCTION get_latest_snapshot IS 'Get most recent snapshot for an instrument';
COMMENT ON FUNCTION cleanup_old_snapshots IS 'Remove old snapshots, keeping only the most recent N';
