// Package outbox implements the transactional outbox pattern: domain events are written to
// outbox_messages in the same transaction as the state change they describe, and a separate
// relay publishes them to Kafka.
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Message is a domain event pending publication.
type Message struct {
	Topic         string
	EventType     string
	AggregateID   string
	Payload       json.RawMessage
	CorrelationID string
}

// Store writes outbox messages as part of the caller's business transaction.
type Store struct{}

// NewStore creates an outbox Store.
func NewStore() *Store {
	return &Store{}
}

// Enqueue inserts msg into outbox_messages within tx, so it is committed atomically with the
// business state change it originates from.
func (s *Store) Enqueue(ctx context.Context, tx *sql.Tx, msg Message) error {
	const query = `
		INSERT INTO outbox_messages (id, topic, event_type, aggregate_id, payload, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	var correlationID sql.NullString
	if msg.CorrelationID != "" {
		correlationID = sql.NullString{String: msg.CorrelationID, Valid: true}
	}

	_, err := tx.ExecContext(ctx, query,
		uuid.New().String(),
		msg.Topic,
		msg.EventType,
		msg.AggregateID,
		[]byte(msg.Payload),
		correlationID,
	)
	if err != nil {
		return fmt.Errorf("failed to enqueue outbox message: %w", err)
	}

	return nil
}
