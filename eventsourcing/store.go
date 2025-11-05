package eventsourcing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ============================================
// Event Store Interface
// ============================================

// Store defines the interface for event persistence
type Store interface {
	// Append adds a new event to the store
	Append(ctx context.Context, event Event) error

	// LoadEvents retrieves all events for an aggregate
	LoadEvents(ctx context.Context, aggregateID, aggregateType string) ([]Event, error)

	// LoadEventsSince retrieves events after a specific version
	LoadEventsSince(ctx context.Context, aggregateID, aggregateType string, version int) ([]Event, error)

	// SaveSnapshot stores a snapshot for performance optimization
	SaveSnapshot(ctx context.Context, aggregateID, aggregateType string, version int, state interface{}) error

	// LoadSnapshot retrieves the latest snapshot
	LoadSnapshot(ctx context.Context, aggregateID, aggregateType string) (version int, state []byte, err error)
}

// ============================================
// PostgreSQL Implementation
// ============================================

// PostgresEventStore implements Store using PostgreSQL
type PostgresEventStore struct {
	db *sql.DB
}

// NewPostgresEventStore creates a new PostgreSQL event store
func NewPostgresEventStore(db *sql.DB) *PostgresEventStore {
	return &PostgresEventStore{db: db}
}

// Append persists an event to the database
func (s *PostgresEventStore) Append(ctx context.Context, event Event) error {
	// Serialize event to JSON
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Extract metadata
	metadata, causationID, correlationID := extractEventMetadata(event)

	// Insert into events table
	query := `
		INSERT INTO events (
			event_id, event_type, aggregate_id, aggregate_type,
			payload, version, timestamp, metadata, causation_id, correlation_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err = s.db.ExecContext(ctx, query,
		event.GetEventID(),
		event.GetEventType(),
		event.GetAggregateID(),
		event.GetAggregateType(),
		payload,
		event.GetVersion(),
		event.GetTimestamp(),
		toJSONB(metadata),
		uuidToNullable(causationID),
		uuidToNullable(correlationID),
	)

	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	return nil
}

// LoadEvents retrieves all events for an aggregate in order
func (s *PostgresEventStore) LoadEvents(ctx context.Context, aggregateID, aggregateType string) ([]Event, error) {
	query := `
		SELECT event_type, payload
		FROM events
		WHERE aggregate_id = $1 AND aggregate_type = $2
		ORDER BY version ASC
	`

	rows, err := s.db.QueryContext(ctx, query, aggregateID, aggregateType)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var eventType string
		var payload []byte

		if err := rows.Scan(&eventType, &payload); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Deserialize event based on type
		event, err := deserializeEvent(eventType, payload)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize event: %w", err)
		}

		events = append(events, event)
	}

	return events, nil
}

// LoadEventsSince retrieves events after a specific version
func (s *PostgresEventStore) LoadEventsSince(ctx context.Context, aggregateID, aggregateType string, version int) ([]Event, error) {
	query := `
		SELECT event_type, payload
		FROM events
		WHERE aggregate_id = $1 AND aggregate_type = $2 AND version > $3
		ORDER BY version ASC
	`

	rows, err := s.db.QueryContext(ctx, query, aggregateID, aggregateType, version)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var eventType string
		var payload []byte

		if err := rows.Scan(&eventType, &payload); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		event, err := deserializeEvent(eventType, payload)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize event: %w", err)
		}

		events = append(events, event)
	}

	return events, nil
}

// SaveSnapshot stores a snapshot for faster replay
func (s *PostgresEventStore) SaveSnapshot(ctx context.Context, aggregateID, aggregateType string, version int, state interface{}) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	query := `
		INSERT INTO snapshots (aggregate_id, aggregate_type, version, state, timestamp)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (aggregate_id, aggregate_type, version) DO UPDATE
		SET state = EXCLUDED.state, timestamp = EXCLUDED.timestamp
	`

	_, err = s.db.ExecContext(ctx, query, aggregateID, aggregateType, version, stateJSON, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	return nil
}

// LoadSnapshot retrieves the latest snapshot
func (s *PostgresEventStore) LoadSnapshot(ctx context.Context, aggregateID, aggregateType string) (int, []byte, error) {
	query := `
		SELECT version, state
		FROM snapshots
		WHERE aggregate_id = $1 AND aggregate_type = $2
		ORDER BY version DESC
		LIMIT 1
	`

	var version int
	var state []byte

	err := s.db.QueryRowContext(ctx, query, aggregateID, aggregateType).Scan(&version, &state)
	if err == sql.ErrNoRows {
		return 0, nil, nil // No snapshot found
	}
	if err != nil {
		return 0, nil, fmt.Errorf("failed to load snapshot: %w", err)
	}

	return version, state, nil
}

// ============================================
// Helper Functions
// ============================================

// deserializeEvent converts JSON payload to concrete event type
func deserializeEvent(eventType string, payload []byte) (Event, error) {
	switch eventType {
	case "OrderPlaced":
		var event OrderPlacedEvent
		err := json.Unmarshal(payload, &event)
		return &event, err

	case "OrderCancelled":
		var event OrderCancelledEvent
		err := json.Unmarshal(payload, &event)
		return &event, err

	case "OrderPartiallyFilled":
		var event OrderPartiallyFilledEvent
		err := json.Unmarshal(payload, &event)
		return &event, err

	case "OrderFilled":
		var event OrderFilledEvent
		err := json.Unmarshal(payload, &event)
		return &event, err

	case "TradeExecuted":
		var event TradeExecutedEvent
		err := json.Unmarshal(payload, &event)
		return &event, err

	default:
		return nil, fmt.Errorf("unknown event type: %s", eventType)
	}
}

// extractEventMetadata extracts metadata from event
func extractEventMetadata(event Event) (map[string]interface{}, *uuid.UUID, *uuid.UUID) {
	switch e := event.(type) {
	case *OrderPlacedEvent:
		return e.Metadata, e.CausationID, e.CorrelationID
	case *OrderCancelledEvent:
		return e.Metadata, e.CausationID, e.CorrelationID
	case *TradeExecutedEvent:
		return e.Metadata, e.CausationID, e.CorrelationID
	default:
		return nil, nil, nil
	}
}

// toJSONB converts map to JSONB or NULL
func toJSONB(m map[string]interface{}) interface{} {
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}

// uuidToNullable converts UUID pointer to nullable UUID
func uuidToNullable(id *uuid.UUID) interface{} {
	if id == nil {
		return nil
	}
	return *id
}
