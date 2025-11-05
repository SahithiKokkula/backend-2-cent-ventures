-- ============================================================================
-- Order Events Schema - Append-Only Event Sourcing
-- ============================================================================
-- This schema implements event sourcing for all order state changes.
-- Every order operation (insert, update, fill, cancel) creates an immutable event.
-- State can be rebuilt by replaying events in sequence.

-- ============================================================================
-- Core Tables
-- ============================================================================

-- order_events: Append-only event log for all order state changes
CREATE TABLE IF NOT EXISTS order_events (
    event_id BIGSERIAL PRIMARY KEY,
    order_id UUID NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    event_data JSONB NOT NULL,
    event_timestamp TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    sequence_number BIGINT NOT NULL,
    
    -- Metadata
    triggered_by VARCHAR(255),  -- What triggered this event (trade_id, user_action, etc.)
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Constraints
    CHECK (event_type IN ('ORDER_CREATED', 'ORDER_FILLED', 'ORDER_PARTIALLY_FILLED', 
                          'ORDER_CANCELLED', 'ORDER_REJECTED', 'ORDER_UPDATED')),
    
    -- Ensure sequence is unique per order
    UNIQUE (order_id, sequence_number)
);

-- orders_snapshot: Current state snapshot (rebuilt from events)
-- This is a materialized view for query performance
CREATE TABLE IF NOT EXISTS orders_snapshot (
    order_id UUID PRIMARY KEY,
    client_id VARCHAR(255) NOT NULL,
    instrument VARCHAR(50) NOT NULL,
    side VARCHAR(10) NOT NULL,
    type VARCHAR(10) NOT NULL,
    price NUMERIC(20, 8) NOT NULL,
    quantity NUMERIC(20, 8) NOT NULL,
    filled_quantity NUMERIC(20, 8) NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    last_event_sequence BIGINT NOT NULL DEFAULT 0,
    
    CHECK (side IN ('buy', 'sell')),
    CHECK (type IN ('limit', 'market')),
    CHECK (status IN ('open', 'partially_filled', 'filled', 'cancelled', 'rejected')),
    CHECK (filled_quantity >= 0),
    CHECK (filled_quantity <= quantity),
    CHECK (quantity > 0),
    CHECK (price >= 0)
);

-- ============================================================================
-- Indexes for Performance
-- ============================================================================

-- Event retrieval by order
CREATE INDEX IF NOT EXISTS idx_order_events_order_id ON order_events(order_id, sequence_number);

-- Event retrieval by timestamp (for replay)
CREATE INDEX IF NOT EXISTS idx_order_events_timestamp ON order_events(event_timestamp);

-- Event retrieval by type
CREATE INDEX IF NOT EXISTS idx_order_events_type ON order_events(event_type);

-- Snapshot queries
CREATE INDEX IF NOT EXISTS idx_orders_snapshot_client_id ON orders_snapshot(client_id);
CREATE INDEX IF NOT EXISTS idx_orders_snapshot_instrument ON orders_snapshot(instrument);
CREATE INDEX IF NOT EXISTS idx_orders_snapshot_status ON orders_snapshot(status);
CREATE INDEX IF NOT EXISTS idx_orders_snapshot_instrument_status ON orders_snapshot(instrument, status);
CREATE INDEX IF NOT EXISTS idx_orders_snapshot_created_at ON orders_snapshot(created_at DESC);

-- ============================================================================
-- Functions for Event Recording
-- ============================================================================

-- Function to get next sequence number for an order
CREATE OR REPLACE FUNCTION get_next_sequence_number(p_order_id UUID)
RETURNS BIGINT AS $$
DECLARE
    v_next_seq BIGINT;
BEGIN
    SELECT COALESCE(MAX(sequence_number), 0) + 1
    INTO v_next_seq
    FROM order_events
    WHERE order_id = p_order_id;
    
    RETURN v_next_seq;
END;
$$ LANGUAGE plpgsql;

-- Function to record ORDER_CREATED event
CREATE OR REPLACE FUNCTION record_order_created(
    p_order_id UUID,
    p_client_id VARCHAR(255),
    p_instrument VARCHAR(50),
    p_side VARCHAR(10),
    p_type VARCHAR(10),
    p_price NUMERIC(20, 8),
    p_quantity NUMERIC(20, 8),
    p_timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW()
)
RETURNS BIGINT AS $$
DECLARE
    v_event_id BIGINT;
    v_sequence BIGINT;
BEGIN
    -- Get sequence number
    v_sequence := get_next_sequence_number(p_order_id);
    
    -- Insert event
    INSERT INTO order_events (
        order_id, event_type, event_data, event_timestamp, sequence_number, triggered_by
    ) VALUES (
        p_order_id,
        'ORDER_CREATED',
        jsonb_build_object(
            'client_id', p_client_id,
            'instrument', p_instrument,
            'side', p_side,
            'type', p_type,
            'price', p_price::TEXT,
            'quantity', p_quantity::TEXT,
            'status', 'open'
        ),
        p_timestamp,
        v_sequence,
        'system'
    ) RETURNING event_id INTO v_event_id;
    
    -- Update snapshot
    INSERT INTO orders_snapshot (
        order_id, client_id, instrument, side, type, price, quantity,
        filled_quantity, status, created_at, updated_at, last_event_sequence
    ) VALUES (
        p_order_id, p_client_id, p_instrument, p_side, p_type, p_price, p_quantity,
        0, 'open', p_timestamp, p_timestamp, v_sequence
    )
    ON CONFLICT (order_id) DO NOTHING;
    
    RETURN v_event_id;
END;
$$ LANGUAGE plpgsql;

-- Function to record ORDER_FILLED or ORDER_PARTIALLY_FILLED event
CREATE OR REPLACE FUNCTION record_order_fill(
    p_order_id UUID,
    p_fill_quantity NUMERIC(20, 8),
    p_new_filled_quantity NUMERIC(20, 8),
    p_new_status VARCHAR(20),
    p_trade_id UUID DEFAULT NULL,
    p_timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW()
)
RETURNS BIGINT AS $$
DECLARE
    v_event_id BIGINT;
    v_sequence BIGINT;
    v_event_type VARCHAR(50);
BEGIN
    -- Determine event type
    v_event_type := CASE 
        WHEN p_new_status = 'filled' THEN 'ORDER_FILLED'
        ELSE 'ORDER_PARTIALLY_FILLED'
    END;
    
    -- Get sequence number
    v_sequence := get_next_sequence_number(p_order_id);
    
    -- Insert event
    INSERT INTO order_events (
        order_id, event_type, event_data, event_timestamp, sequence_number, triggered_by
    ) VALUES (
        p_order_id,
        v_event_type,
        jsonb_build_object(
            'fill_quantity', p_fill_quantity::TEXT,
            'filled_quantity', p_new_filled_quantity::TEXT,
            'status', p_new_status,
            'trade_id', p_trade_id
        ),
        p_timestamp,
        v_sequence,
        COALESCE(p_trade_id::TEXT, 'system')
    ) RETURNING event_id INTO v_event_id;
    
    -- Update snapshot
    UPDATE orders_snapshot
    SET filled_quantity = p_new_filled_quantity,
        status = p_new_status,
        updated_at = p_timestamp,
        last_event_sequence = v_sequence
    WHERE order_id = p_order_id;
    
    RETURN v_event_id;
END;
$$ LANGUAGE plpgsql;

-- Function to record ORDER_CANCELLED event
CREATE OR REPLACE FUNCTION record_order_cancelled(
    p_order_id UUID,
    p_reason VARCHAR(255) DEFAULT NULL,
    p_timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW()
)
RETURNS BIGINT AS $$
DECLARE
    v_event_id BIGINT;
    v_sequence BIGINT;
BEGIN
    -- Get sequence number
    v_sequence := get_next_sequence_number(p_order_id);
    
    -- Insert event
    INSERT INTO order_events (
        order_id, event_type, event_data, event_timestamp, sequence_number, triggered_by
    ) VALUES (
        p_order_id,
        'ORDER_CANCELLED',
        jsonb_build_object(
            'status', 'cancelled',
            'reason', p_reason
        ),
        p_timestamp,
        v_sequence,
        'user'
    ) RETURNING event_id INTO v_event_id;
    
    -- Update snapshot
    UPDATE orders_snapshot
    SET status = 'cancelled',
        updated_at = p_timestamp,
        last_event_sequence = v_sequence
    WHERE order_id = p_order_id;
    
    RETURN v_event_id;
END;
$$ LANGUAGE plpgsql;

-- Function to record ORDER_REJECTED event
CREATE OR REPLACE FUNCTION record_order_rejected(
    p_order_id UUID,
    p_reason VARCHAR(255),
    p_timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW()
)
RETURNS BIGINT AS $$
DECLARE
    v_event_id BIGINT;
    v_sequence BIGINT;
BEGIN
    -- Get sequence number
    v_sequence := get_next_sequence_number(p_order_id);
    
    -- Insert event
    INSERT INTO order_events (
        order_id, event_type, event_data, event_timestamp, sequence_number, triggered_by
    ) VALUES (
        p_order_id,
        'ORDER_REJECTED',
        jsonb_build_object(
            'status', 'rejected',
            'reason', p_reason
        ),
        p_timestamp,
        v_sequence,
        'system'
    ) RETURNING event_id INTO v_event_id;
    
    -- Update snapshot
    UPDATE orders_snapshot
    SET status = 'rejected',
        updated_at = p_timestamp,
        last_event_sequence = v_sequence
    WHERE order_id = p_order_id;
    
    RETURN v_event_id;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- State Replay Functions
-- ============================================================================

-- Function to replay events for a single order
CREATE OR REPLACE FUNCTION replay_order_state(p_order_id UUID)
RETURNS TABLE (
    order_id UUID,
    client_id VARCHAR(255),
    instrument VARCHAR(50),
    side VARCHAR(10),
    type VARCHAR(10),
    price NUMERIC(20, 8),
    quantity NUMERIC(20, 8),
    filled_quantity NUMERIC(20, 8),
    status VARCHAR(20),
    created_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE,
    event_count BIGINT
) AS $$
DECLARE
    v_state RECORD;
    v_event RECORD;
    v_event_count BIGINT := 0;
BEGIN
    -- Initialize state variables
    v_state.order_id := p_order_id;
    v_state.filled_quantity := 0;
    v_state.status := 'open';
    
    -- Replay all events in sequence
    FOR v_event IN
        SELECT event_type, event_data, event_timestamp, sequence_number
        FROM order_events
        WHERE order_events.order_id = p_order_id
        ORDER BY sequence_number ASC
    LOOP
        v_event_count := v_event_count + 1;
        
        -- Apply event based on type
        CASE v_event.event_type
            WHEN 'ORDER_CREATED' THEN
                v_state.client_id := v_event.event_data->>'client_id';
                v_state.instrument := v_event.event_data->>'instrument';
                v_state.side := v_event.event_data->>'side';
                v_state.type := v_event.event_data->>'type';
                v_state.price := (v_event.event_data->>'price')::NUMERIC(20, 8);
                v_state.quantity := (v_event.event_data->>'quantity')::NUMERIC(20, 8);
                v_state.created_at := v_event.event_timestamp;
                v_state.updated_at := v_event.event_timestamp;
                v_state.status := 'open';
                
            WHEN 'ORDER_FILLED', 'ORDER_PARTIALLY_FILLED' THEN
                v_state.filled_quantity := (v_event.event_data->>'filled_quantity')::NUMERIC(20, 8);
                v_state.status := v_event.event_data->>'status';
                v_state.updated_at := v_event.event_timestamp;
                
            WHEN 'ORDER_CANCELLED' THEN
                v_state.status := 'cancelled';
                v_state.updated_at := v_event.event_timestamp;
                
            WHEN 'ORDER_REJECTED' THEN
                v_state.status := 'rejected';
                v_state.updated_at := v_event.event_timestamp;
        END CASE;
    END LOOP;
    
    -- Return final state
    RETURN QUERY SELECT 
        v_state.order_id,
        v_state.client_id,
        v_state.instrument,
        v_state.side,
        v_state.type,
        v_state.price,
        v_state.quantity,
        v_state.filled_quantity,
        v_state.status,
        v_state.created_at,
        v_state.updated_at,
        v_event_count;
END;
$$ LANGUAGE plpgsql;

-- Function to rebuild entire snapshot table from events
CREATE OR REPLACE FUNCTION rebuild_orders_snapshot()
RETURNS BIGINT AS $$
DECLARE
    v_order_id UUID;
    v_replayed_state RECORD;
    v_count BIGINT := 0;
BEGIN
    -- Clear current snapshot
    TRUNCATE orders_snapshot;
    
    -- Get all unique order IDs
    FOR v_order_id IN
        SELECT DISTINCT order_events.order_id
        FROM order_events
        ORDER BY order_events.order_id
    LOOP
        -- Replay state for this order
        SELECT * INTO v_replayed_state
        FROM replay_order_state(v_order_id)
        LIMIT 1;
        
        -- Insert into snapshot
        INSERT INTO orders_snapshot (
            order_id, client_id, instrument, side, type, price, quantity,
            filled_quantity, status, created_at, updated_at, last_event_sequence
        ) VALUES (
            v_replayed_state.order_id,
            v_replayed_state.client_id,
            v_replayed_state.instrument,
            v_replayed_state.side,
            v_replayed_state.type,
            v_replayed_state.price,
            v_replayed_state.quantity,
            v_replayed_state.filled_quantity,
            v_replayed_state.status,
            v_replayed_state.created_at,
            v_replayed_state.updated_at,
            v_replayed_state.event_count
        );
        
        v_count := v_count + 1;
    END LOOP;
    
    RETURN v_count;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- Utility Functions
-- ============================================================================

-- Get order history (all events)
CREATE OR REPLACE FUNCTION get_order_history(p_order_id UUID)
RETURNS TABLE (
    event_id BIGINT,
    event_type VARCHAR(50),
    event_data JSONB,
    event_timestamp TIMESTAMP WITH TIME ZONE,
    sequence_number BIGINT,
    triggered_by VARCHAR(255)
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        e.event_id,
        e.event_type,
        e.event_data,
        e.event_timestamp,
        e.sequence_number,
        e.triggered_by
    FROM order_events e
    WHERE e.order_id = p_order_id
    ORDER BY e.sequence_number ASC;
END;
$$ LANGUAGE plpgsql;

-- Verify snapshot consistency with events
CREATE OR REPLACE FUNCTION verify_snapshot_consistency()
RETURNS TABLE (
    order_id UUID,
    snapshot_status VARCHAR(20),
    replayed_status VARCHAR(20),
    snapshot_filled NUMERIC(20, 8),
    replayed_filled NUMERIC(20, 8),
    is_consistent BOOLEAN
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        s.order_id,
        s.status as snapshot_status,
        r.status as replayed_status,
        s.filled_quantity as snapshot_filled,
        r.filled_quantity as replayed_filled,
        (s.status = r.status AND s.filled_quantity = r.filled_quantity) as is_consistent
    FROM orders_snapshot s
    CROSS JOIN LATERAL replay_order_state(s.order_id) r
    WHERE s.status != r.status OR s.filled_quantity != r.filled_quantity;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- Comments and Documentation
-- ============================================================================

COMMENT ON TABLE order_events IS 'Append-only event log for all order state changes. Immutable once written.';
COMMENT ON TABLE orders_snapshot IS 'Current state snapshot rebuilt from events. Can be regenerated at any time.';
COMMENT ON FUNCTION record_order_created IS 'Record ORDER_CREATED event and initialize snapshot';
COMMENT ON FUNCTION record_order_fill IS 'Record ORDER_FILLED or ORDER_PARTIALLY_FILLED event';
COMMENT ON FUNCTION record_order_cancelled IS 'Record ORDER_CANCELLED event';
COMMENT ON FUNCTION record_order_rejected IS 'Record ORDER_REJECTED event';
COMMENT ON FUNCTION replay_order_state IS 'Replay all events for an order to rebuild its state';
COMMENT ON FUNCTION rebuild_orders_snapshot IS 'Rebuild entire snapshot table from events';
COMMENT ON FUNCTION get_order_history IS 'Get complete event history for an order';
COMMENT ON FUNCTION verify_snapshot_consistency IS 'Verify snapshot matches replayed state from events';
