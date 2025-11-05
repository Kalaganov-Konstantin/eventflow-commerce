package eventstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	"github.com/google/uuid"
)

func TestSnapshotStore_SaveSnapshot(t *testing.T) {
	t.Run("writes the aggregate state", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment, err := domain.Initiate(uuid.New(), uuid.New(), 100, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_snapshots").
			WithArgs(payment.ID, payment.Version, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}

		store := NewSnapshotStore(db)
		if err := store.SaveSnapshot(context.Background(), tx, payment); err != nil {
			t.Fatalf("SaveSnapshot() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error on exec failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment, err := domain.Initiate(uuid.New(), uuid.New(), 100, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_snapshots").WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		store := NewSnapshotStore(db)
		if err := store.SaveSnapshot(context.Background(), tx, payment); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestSnapshotStore_LatestSnapshot(t *testing.T) {
	t.Run("returns the most recent snapshot", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment, err := domain.Initiate(uuid.New(), uuid.New(), 100, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		payment.ClearPendingEvents()
		data, err := json.Marshal(payment)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		rows := sqlmock.NewRows([]string{"aggregate_version", "snapshot_data"}).AddRow(1, data)
		mock.ExpectQuery("FROM payment_snapshots").WithArgs(payment.ID).WillReturnRows(rows)

		store := NewSnapshotStore(db)
		got, err := store.LatestSnapshot(context.Background(), payment.ID)
		if err != nil {
			t.Fatalf("LatestSnapshot() error = %v", err)
		}
		if got == nil {
			t.Fatal("LatestSnapshot() = nil, want a snapshot")
		}
		if got.AggregateVersion != 1 {
			t.Errorf("AggregateVersion = %v, want 1", got.AggregateVersion)
		}
		if got.Payment.ID != payment.ID || got.Payment.Status != payment.Status || got.Payment.AmountCents != payment.AmountCents {
			t.Errorf("Payment = %+v, want %+v", got.Payment, payment)
		}
	})

	t.Run("returns nil when there is no snapshot", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		mock.ExpectQuery("FROM payment_snapshots").WithArgs(aggregateID).WillReturnError(sql.ErrNoRows)

		store := NewSnapshotStore(db)
		got, err := store.LatestSnapshot(context.Background(), aggregateID)
		if err != nil {
			t.Fatalf("LatestSnapshot() error = %v", err)
		}
		if got != nil {
			t.Errorf("LatestSnapshot() = %+v, want nil", got)
		}
	})

	t.Run("returns error on query failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		mock.ExpectQuery("FROM payment_snapshots").WithArgs(aggregateID).WillReturnError(errors.New("boom"))

		store := NewSnapshotStore(db)
		if _, err := store.LatestSnapshot(context.Background(), aggregateID); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}
