package projection

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	"github.com/google/uuid"
)

func TestPaymentStatus_Apply(t *testing.T) {
	t.Run("upserts an initiated payment without completing it", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment, err := domain.Initiate(uuid.New(), uuid.New(), 4999, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		event := payment.PendingEvents()[0]

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_status").
			WithArgs(payment.ID, payment.CustomerID, payment.OrderID, payment.AmountCents, payment.Currency,
				string(domain.StatusInitiated), sqlmock.AnyArg(), sqlmock.AnyArg(), payment.Version).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}

		projector := NewPaymentStatus()
		if err := projector.Apply(context.Background(), tx, payment, event); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("fills completed_at and status on a processed payment", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment, err := domain.Initiate(uuid.New(), uuid.New(), 4999, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		if err := payment.Process("txn_1"); err != nil {
			t.Fatalf("Process: %v", err)
		}
		event := payment.PendingEvents()[1]

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_status").
			WithArgs(payment.ID, payment.CustomerID, payment.OrderID, payment.AmountCents, payment.Currency,
				string(domain.StatusCompleted), "txn_1", sqlmock.AnyArg(), payment.Version).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}

		projector := NewPaymentStatus()
		if err := projector.Apply(context.Background(), tx, payment, event); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("advances status without touching completed_at on other events", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment, err := domain.Initiate(uuid.New(), uuid.New(), 4999, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		if err := payment.Fail("insufficient_funds"); err != nil {
			t.Fatalf("Fail: %v", err)
		}
		event := payment.PendingEvents()[1]

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_status").
			WithArgs(payment.ID, payment.CustomerID, payment.OrderID, payment.AmountCents, payment.Currency,
				string(domain.StatusFailed), sqlmock.AnyArg(), sqlmock.AnyArg(), payment.Version).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}

		projector := NewPaymentStatus()
		if err := projector.Apply(context.Background(), tx, payment, event); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("wraps exec errors", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		payment, err := domain.Initiate(uuid.New(), uuid.New(), 4999, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		event := payment.PendingEvents()[0]

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO payment_status").WillReturnError(errors.New("boom"))

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		projector := NewPaymentStatus()
		if err := projector.Apply(context.Background(), tx, payment, event); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}
