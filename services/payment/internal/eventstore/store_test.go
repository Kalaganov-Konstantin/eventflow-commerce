package eventstore

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestStore_Append(t *testing.T) {
	t.Run("writes events as consecutive versions", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		events := []domain.Event{
			&domain.PaymentInitiated{PaymentID: aggregateID, AmountCents: 100, Currency: "USD"},
			&domain.PaymentProcessed{PaymentID: aggregateID, TransactionID: "txn_1"},
		}

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_events").
			WithArgs(aggregateID, domain.EventTypePaymentInitiated, 1, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO payment_events").
			WithArgs(aggregateID, domain.EventTypePaymentProcessed, 2, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}

		store := NewStore(db)
		if err := store.Append(context.Background(), tx, aggregateID, 0, events); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns conflict on a version collision", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		events := []domain.Event{&domain.PaymentFailed{PaymentID: aggregateID, Reason: "declined"}}

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_events").
			WithArgs(aggregateID, domain.EventTypePaymentFailed, 1, sqlmock.AnyArg()).
			WillReturnError(&pq.Error{Code: "23505"})
		mock.ExpectRollback()

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		store := NewStore(db)
		err = store.Append(context.Background(), tx, aggregateID, 0, events)
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "CONFLICT" {
			t.Errorf("error = %v, want CONFLICT", err)
		}
	})

	t.Run("wraps other exec errors", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		events := []domain.Event{&domain.PaymentFailed{PaymentID: aggregateID, Reason: "declined"}}

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_events").
			WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		store := NewStore(db)
		if err := store.Append(context.Background(), tx, aggregateID, 0, events); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestStore_Load(t *testing.T) {
	t.Run("returns events ordered by version", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		initiated := &domain.PaymentInitiated{PaymentID: aggregateID, AmountCents: 100, Currency: "USD"}
		processed := &domain.PaymentProcessed{PaymentID: aggregateID, TransactionID: "txn_1"}

		initiatedData, err := domain.MarshalEvent(initiated)
		if err != nil {
			t.Fatalf("MarshalEvent: %v", err)
		}
		processedData, err := domain.MarshalEvent(processed)
		if err != nil {
			t.Fatalf("MarshalEvent: %v", err)
		}

		rows := sqlmock.NewRows([]string{"event_type", "event_data"}).
			AddRow(domain.EventTypePaymentInitiated, initiatedData).
			AddRow(domain.EventTypePaymentProcessed, processedData)
		mock.ExpectQuery("FROM payment_events").WithArgs(aggregateID, 0).WillReturnRows(rows)

		store := NewStore(db)
		got, err := store.Load(context.Background(), aggregateID, 0)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2", len(got))
		}
		gotInitiated, ok := got[0].(*domain.PaymentInitiated)
		if !ok || *gotInitiated != *initiated {
			t.Errorf("got[0] = %#v, want %#v", got[0], initiated)
		}
		gotProcessed, ok := got[1].(*domain.PaymentProcessed)
		if !ok || *gotProcessed != *processed {
			t.Errorf("got[1] = %#v, want %#v", got[1], processed)
		}
	})

	t.Run("returns error on query failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		mock.ExpectQuery("FROM payment_events").WithArgs(aggregateID, 0).WillReturnError(errors.New("boom"))

		store := NewStore(db)
		if _, err := store.Load(context.Background(), aggregateID, 0); err == nil {
			t.Fatal("expected error, got none")
		}
	})

	t.Run("returns error on an unknown stored event type", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		aggregateID := uuid.New()
		rows := sqlmock.NewRows([]string{"event_type", "event_data"}).AddRow("payment.unknown", []byte(`{}`))
		mock.ExpectQuery("FROM payment_events").WithArgs(aggregateID, 0).WillReturnRows(rows)

		store := NewStore(db)
		if _, err := store.Load(context.Background(), aggregateID, 0); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}
