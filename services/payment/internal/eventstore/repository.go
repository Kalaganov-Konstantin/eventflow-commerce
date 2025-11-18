package eventstore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/projection"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// Repository rebuilds and persists payment aggregates through their event history and snapshots.
type Repository struct {
	db        *sql.DB
	events    *Store
	snapshots *SnapshotStore
	status    *projection.PaymentStatus
}

// NewRepository builds a repository backed by the given database handle.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db:        db,
		events:    NewStore(db),
		snapshots: NewSnapshotStore(db),
		status:    projection.NewPaymentStatus(),
	}
}

// FindByOrderID rebuilds the payment aggregate initiated for orderID, or returns nil if none exists.
func (r *Repository) FindByOrderID(ctx context.Context, orderID uuid.UUID) (*domain.Payment, error) {
	aggregateID, err := r.events.FindByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if aggregateID == uuid.Nil {
		return nil, nil
	}
	return r.Load(ctx, aggregateID)
}

// Load rebuilds a payment aggregate from its latest snapshot, if any, plus every event recorded since.
func (r *Repository) Load(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	snapshot, err := r.snapshots.LatestSnapshot(ctx, id)
	if err != nil {
		return nil, err
	}

	payment := &domain.Payment{}
	fromVersion := 0
	if snapshot != nil {
		payment = &snapshot.Payment
		fromVersion = snapshot.AggregateVersion
	}

	events, err := r.events.Load(ctx, id, fromVersion)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		payment.Apply(event)
	}
	payment.ClearPendingEvents()

	if payment.ID == uuid.Nil {
		return nil, apperrors.NewNotFound("payment")
	}
	return payment, nil
}

// Save appends the aggregate's pending events in one transaction and writes a new snapshot every
// SnapshotThreshold versions. A no-op when the aggregate has no pending events.
func (r *Repository) Save(ctx context.Context, payment *domain.Payment) error {
	newEvents := payment.PendingEvents()
	if len(newEvents) == 0 {
		return nil
	}
	expectedVersion := payment.Version - len(newEvents)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := r.events.Append(ctx, tx, payment.ID, expectedVersion, newEvents); err != nil {
		return err
	}

	for _, event := range newEvents {
		if err := r.status.Apply(ctx, tx, payment, event); err != nil {
			return err
		}
	}

	if payment.Version%SnapshotThreshold == 0 {
		if err := r.snapshots.SaveSnapshot(ctx, tx, payment); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	payment.ClearPendingEvents()
	return nil
}
