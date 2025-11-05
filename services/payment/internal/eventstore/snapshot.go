package eventstore

import (
	"context"
	"database/sql"
	"encoding/json"
	stderrors "errors"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	"github.com/google/uuid"
)

// SnapshotThreshold is how many events accumulate between payment aggregate snapshots.
const SnapshotThreshold = 10

// Snapshot is a point-in-time capture of a payment aggregate's state.
type Snapshot struct {
	AggregateID      uuid.UUID
	AggregateVersion int
	Payment          domain.Payment
}

// SnapshotStore persists and retrieves payment aggregate snapshots in postgres.
type SnapshotStore struct {
	db *sql.DB
}

// NewSnapshotStore builds a snapshot store backed by the given database handle.
func NewSnapshotStore(db *sql.DB) *SnapshotStore {
	return &SnapshotStore{db: db}
}

// SaveSnapshot writes the aggregate's current state as a new snapshot row, inside tx.
func (s *SnapshotStore) SaveSnapshot(ctx context.Context, tx *sql.Tx, payment *domain.Payment) error {
	data, err := json.Marshal(payment)
	if err != nil {
		return fmt.Errorf("marshal payment snapshot: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payment_snapshots (aggregate_id, aggregate_version, snapshot_data)
		VALUES ($1, $2, $3)
	`, payment.ID, payment.Version, data); err != nil {
		return fmt.Errorf("insert payment snapshot: %w", err)
	}
	return nil
}

// LatestSnapshot returns the most recent snapshot for aggregateID, or nil if none exists.
func (s *SnapshotStore) LatestSnapshot(ctx context.Context, aggregateID uuid.UUID) (*Snapshot, error) {
	var data []byte
	snapshot := &Snapshot{AggregateID: aggregateID}

	err := s.db.QueryRowContext(ctx, `
		SELECT aggregate_version, snapshot_data FROM payment_snapshots
		WHERE aggregate_id = $1
		ORDER BY aggregate_version DESC
		LIMIT 1
	`, aggregateID).Scan(&snapshot.AggregateVersion, &data)
	if stderrors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select payment snapshot: %w", err)
	}

	if err := json.Unmarshal(data, &snapshot.Payment); err != nil {
		return nil, fmt.Errorf("unmarshal payment snapshot: %w", err)
	}
	return snapshot, nil
}
