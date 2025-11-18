package eventstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// buildHistory returns a payment's aggregate id and the three events that take it from
// initiated through completed to refunded.
func buildHistory(t *testing.T) (uuid.UUID, []domain.Event) {
	t.Helper()

	payment, err := domain.Initiate(uuid.New(), uuid.New(), 4999, "USD")
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if err := payment.Process("txn_1"); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if err := payment.Refund("customer_request"); err != nil {
		t.Fatalf("Refund: %v", err)
	}
	return payment.ID, payment.PendingEvents()
}

func eventRows(t *testing.T, events []domain.Event) *sqlmock.Rows {
	t.Helper()

	rows := sqlmock.NewRows([]string{"event_type", "event_data"})
	for _, event := range events {
		data, err := domain.MarshalEvent(event)
		if err != nil {
			t.Fatalf("MarshalEvent: %v", err)
		}
		rows.AddRow(event.EventType(), data)
	}
	return rows
}

// loadWithMocks builds a Repository over a fresh sqlmock database, wires the snapshot lookup to return
// snapshot (or none, when nil) and the event lookup to return tailEvents, then loads the aggregate.
func loadWithMocks(t *testing.T, aggregateID uuid.UUID, snapshot *domain.Payment, tailEvents []domain.Event) *domain.Payment {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	fromVersion := 0
	if snapshot == nil {
		mock.ExpectQuery("FROM payment_snapshots").WithArgs(aggregateID).WillReturnError(sql.ErrNoRows)
	} else {
		data, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		fromVersion = snapshot.Version
		mock.ExpectQuery("FROM payment_snapshots").WithArgs(aggregateID).WillReturnRows(
			sqlmock.NewRows([]string{"aggregate_version", "snapshot_data"}).AddRow(snapshot.Version, data))
	}
	mock.ExpectQuery("FROM payment_events").WithArgs(aggregateID, fromVersion).WillReturnRows(eventRows(t, tailEvents))

	repo := NewRepository(db)
	got, err := repo.Load(context.Background(), aggregateID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return got
}

func TestRepository_Load(t *testing.T) {
	t.Run("rebuilds from the full event history when there is no snapshot", func(t *testing.T) {
		aggregateID, events := buildHistory(t)

		got := loadWithMocks(t, aggregateID, nil, events)

		if got.Status != domain.StatusRefunded {
			t.Errorf("Status = %v, want %v", got.Status, domain.StatusRefunded)
		}
		if got.Version != 3 {
			t.Errorf("Version = %v, want 3", got.Version)
		}
		if len(got.PendingEvents()) != 0 {
			t.Errorf("len(PendingEvents()) = %d, want 0", len(got.PendingEvents()))
		}
	})

	t.Run("returns not found when there is no snapshot and no events", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		mock.ExpectQuery("FROM payment_snapshots").WithArgs(aggregateID).WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("FROM payment_events").WithArgs(aggregateID, 0).
			WillReturnRows(sqlmock.NewRows([]string{"event_type", "event_data"}))

		repo := NewRepository(db)
		_, err = repo.Load(context.Background(), aggregateID)
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "NOT_FOUND" {
			t.Errorf("error = %v, want NOT_FOUND", err)
		}
	})

	t.Run("propagates snapshot lookup errors", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		mock.ExpectQuery("FROM payment_snapshots").WithArgs(aggregateID).WillReturnError(errors.New("boom"))

		repo := NewRepository(db)
		if _, err := repo.Load(context.Background(), aggregateID); err == nil {
			t.Fatal("expected error, got none")
		}
	})

	t.Run("propagates event load errors", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		mock.ExpectQuery("FROM payment_snapshots").WithArgs(aggregateID).WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("FROM payment_events").WithArgs(aggregateID, 0).WillReturnError(errors.New("boom"))

		repo := NewRepository(db)
		if _, err := repo.Load(context.Background(), aggregateID); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestRepository_Load_SnapshotMatchesFullHistory(t *testing.T) {
	aggregateID, events := buildHistory(t)

	full := loadWithMocks(t, aggregateID, nil, events)

	snapshotPayment := &domain.Payment{}
	snapshotPayment.Apply(events[0])
	snapshotPayment.ClearPendingEvents()
	fromSnapshot := loadWithMocks(t, aggregateID, snapshotPayment, events[1:])

	if full.ID != fromSnapshot.ID ||
		full.OrderID != fromSnapshot.OrderID ||
		full.CustomerID != fromSnapshot.CustomerID ||
		full.AmountCents != fromSnapshot.AmountCents ||
		full.Currency != fromSnapshot.Currency ||
		full.Status != fromSnapshot.Status ||
		full.Version != fromSnapshot.Version {
		t.Errorf("reconstructed aggregates differ: full history = %+v, snapshot + tail = %+v", full, fromSnapshot)
	}
}

func TestRepository_Save(t *testing.T) {
	t.Run("is a no-op when there are no pending events", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment := &domain.Payment{ID: uuid.New(), Version: 3}

		repo := NewRepository(db)
		if err := repo.Save(context.Background(), payment); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("appends pending events without a snapshot below the threshold", func(t *testing.T) {
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
		mock.ExpectExec("INSERT INTO payment_events").
			WithArgs(payment.ID, domain.EventTypePaymentInitiated, 1, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO payment_status").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		repo := NewRepository(db)
		if err := repo.Save(context.Background(), payment); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		if len(payment.PendingEvents()) != 0 {
			t.Errorf("len(PendingEvents()) = %d, want 0", len(payment.PendingEvents()))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("writes a snapshot when the version reaches the threshold", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment := &domain.Payment{ID: uuid.New(), Status: domain.StatusInitiated, Version: 9}
		if err := payment.Process("txn_1"); err != nil {
			t.Fatalf("Process: %v", err)
		}

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_events").
			WithArgs(payment.ID, domain.EventTypePaymentProcessed, 10, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO payment_status").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO payment_snapshots").
			WithArgs(payment.ID, 10, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		repo := NewRepository(db)
		if err := repo.Save(context.Background(), payment); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error when the transaction fails to begin", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment, err := domain.Initiate(uuid.New(), uuid.New(), 100, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		mock.ExpectBegin().WillReturnError(errors.New("boom"))

		repo := NewRepository(db)
		if err := repo.Save(context.Background(), payment); err == nil {
			t.Fatal("expected error, got none")
		}
	})

	t.Run("rolls back and keeps pending events when append fails", func(t *testing.T) {
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
		mock.ExpectExec("INSERT INTO payment_events").WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		repo := NewRepository(db)
		if err := repo.Save(context.Background(), payment); err == nil {
			t.Fatal("expected error, got none")
		}
		if len(payment.PendingEvents()) == 0 {
			t.Error("pending events should remain after a failed save")
		}
	})

	t.Run("rolls back and keeps pending events when the read model upsert fails", func(t *testing.T) {
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
		mock.ExpectExec("INSERT INTO payment_events").
			WithArgs(payment.ID, domain.EventTypePaymentInitiated, 1, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO payment_status").WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		repo := NewRepository(db)
		if err := repo.Save(context.Background(), payment); err == nil {
			t.Fatal("expected error, got none")
		}
		if len(payment.PendingEvents()) == 0 {
			t.Error("pending events should remain after a failed save")
		}
	})
}

func TestRepository_FindByOrderID(t *testing.T) {
	t.Run("rebuilds the payment initiated for the order", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID, events := buildHistory(t)
		orderID := events[0].(*domain.PaymentInitiated).OrderID

		mock.ExpectQuery("FROM payment_events").
			WithArgs(domain.EventTypePaymentInitiated, orderID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"aggregate_id"}).AddRow(aggregateID))
		mock.ExpectQuery("FROM payment_snapshots").WithArgs(aggregateID).WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("FROM payment_events").WithArgs(aggregateID, 0).WillReturnRows(eventRows(t, events))

		repo := NewRepository(db)
		got, err := repo.FindByOrderID(context.Background(), orderID)
		if err != nil {
			t.Fatalf("FindByOrderID() error = %v", err)
		}
		if got == nil || got.ID != aggregateID {
			t.Errorf("FindByOrderID() = %+v, want payment %v", got, aggregateID)
		}
	})

	t.Run("returns nil when no payment was initiated for the order", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		mock.ExpectQuery("FROM payment_events").
			WithArgs(domain.EventTypePaymentInitiated, orderID.String()).
			WillReturnError(sql.ErrNoRows)

		repo := NewRepository(db)
		got, err := repo.FindByOrderID(context.Background(), orderID)
		if err != nil {
			t.Fatalf("FindByOrderID() error = %v", err)
		}
		if got != nil {
			t.Errorf("FindByOrderID() = %+v, want nil", got)
		}
	})

	t.Run("propagates lookup errors", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		mock.ExpectQuery("FROM payment_events").
			WithArgs(domain.EventTypePaymentInitiated, orderID.String()).
			WillReturnError(errors.New("boom"))

		repo := NewRepository(db)
		if _, err := repo.FindByOrderID(context.Background(), orderID); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}
