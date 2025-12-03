package events

import (
	"context"
	"database/sql"
	"fmt"
)

// ProcessedStore records handled event IDs so consumers can recognize redelivered messages.
type ProcessedStore struct {
	db *sql.DB
}

// NewProcessedStore creates a ProcessedStore backed by db.
func NewProcessedStore(db *sql.DB) *ProcessedStore {
	return &ProcessedStore{db: db}
}

// MarkProcessed records eventID as handled within tx. It returns false when eventID was
// already recorded, meaning the message is a redelivery that should not be reprocessed.
func (s *ProcessedStore) MarkProcessed(ctx context.Context, tx *sql.Tx, eventID, eventType string) (bool, error) {
	const query = `
		INSERT INTO processed_events (event_id, event_type)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
	`

	result, err := tx.ExecContext(ctx, query, eventID, eventType)
	if err != nil {
		return false, fmt.Errorf("failed to mark event processed: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to read rows affected: %w", err)
	}

	return rows > 0, nil
}

// WasProcessed reports whether eventID has already been recorded as processed.
func (s *ProcessedStore) WasProcessed(ctx context.Context, eventID string) (bool, error) {
	const query = `SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)`

	var exists bool
	if err := s.db.QueryRowContext(ctx, query, eventID).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check processed event: %w", err)
	}

	return exists, nil
}

// Idempotent wraps handler so events already recorded in store are skipped instead of
// reprocessed. The actual bookkeeping happens inside handler via MarkProcessed, in the same
// transaction as its business writes.
func Idempotent(store *ProcessedStore, handler func(Event) error) func(Event) error {
	return func(event Event) error {
		processed, err := store.WasProcessed(context.Background(), event.ID)
		if err != nil {
			return fmt.Errorf("failed to check idempotency for event %s: %w", event.ID, err)
		}
		if processed {
			return nil
		}
		return handler(event)
	}
}
