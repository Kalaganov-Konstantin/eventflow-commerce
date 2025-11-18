// Package eventstore persists and rebuilds payment aggregates from their event history.
package eventstore

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

const uniqueViolation = "23505"

// Store is an append only event store for payment aggregates backed by postgres.
type Store struct {
	db *sql.DB
}

// NewStore builds a store backed by the given database handle.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Append writes events for aggregateID as consecutive versions after expectedVersion, inside tx. A
// concurrent writer that already advanced the aggregate past expectedVersion collides with the unique
// index on (aggregate_id, event_version) and surfaces as errors.NewConflict.
func (s *Store) Append(ctx context.Context, tx *sql.Tx, aggregateID uuid.UUID, expectedVersion int, events []domain.Event) error {
	for i, event := range events {
		data, err := domain.MarshalEvent(event)
		if err != nil {
			return err
		}

		version := expectedVersion + i + 1
		_, err = tx.ExecContext(ctx, `
			INSERT INTO payment_events (aggregate_id, aggregate_type, event_type, event_version, event_data)
			VALUES ($1, 'payment', $2, $3, $4)
		`, aggregateID, event.EventType(), version, data)
		if err != nil {
			var pqErr *pq.Error
			if stderrors.As(err, &pqErr) && pqErr.Code == uniqueViolation {
				return apperrors.NewConflict(fmt.Sprintf("payment %s: version %d already exists", aggregateID, version))
			}
			return fmt.Errorf("insert payment event: %w", err)
		}
	}
	return nil
}

// FindByOrderID returns the aggregate id of the payment initiated for orderID, or uuid.Nil if none
// was ever initiated.
func (s *Store) FindByOrderID(ctx context.Context, orderID uuid.UUID) (uuid.UUID, error) {
	var aggregateID uuid.UUID
	err := s.db.QueryRowContext(ctx, `
		SELECT aggregate_id FROM payment_events
		WHERE aggregate_type = 'payment' AND event_type = $1 AND event_data ->> 'order_id' = $2
		LIMIT 1
	`, domain.EventTypePaymentInitiated, orderID.String()).Scan(&aggregateID)
	if stderrors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("select payment by order id: %w", err)
	}
	return aggregateID, nil
}

// Load returns every event recorded for aggregateID after fromVersion, ordered by version.
func (s *Store) Load(ctx context.Context, aggregateID uuid.UUID, fromVersion int) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_type, event_data FROM payment_events
		WHERE aggregate_id = $1 AND aggregate_type = 'payment' AND event_version > $2
		ORDER BY event_version ASC
	`, aggregateID, fromVersion)
	if err != nil {
		return nil, fmt.Errorf("select payment events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []domain.Event
	for rows.Next() {
		var eventType string
		var data []byte
		if err := rows.Scan(&eventType, &data); err != nil {
			return nil, fmt.Errorf("scan payment event: %w", err)
		}

		event, err := domain.UnmarshalEvent(eventType, data)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment events: %w", err)
	}
	return events, nil
}
