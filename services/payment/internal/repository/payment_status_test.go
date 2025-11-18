package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

var paymentStatusColumnNames = []string{
	"id", "customer_id", "order_id", "amount_cents", "currency", "status",
	"transaction_id", "created_at", "updated_at", "completed_at", "version",
}

func newTestPaymentStatus() *PaymentStatus {
	now := time.Now().UTC()
	completedAt := now
	return &PaymentStatus{
		ID:            uuid.New(),
		CustomerID:    uuid.New(),
		OrderID:       uuid.New(),
		AmountCents:   4999,
		Currency:      "USD",
		Status:        "completed",
		TransactionID: "txn_1",
		CreatedAt:     now,
		UpdatedAt:     now,
		CompletedAt:   &completedAt,
		Version:       2,
	}
}

func paymentStatusRow(s *PaymentStatus) *sqlmock.Rows {
	return sqlmock.NewRows(paymentStatusColumnNames).AddRow(
		s.ID, s.CustomerID, s.OrderID, s.AmountCents, s.Currency, s.Status,
		s.TransactionID, s.CreatedAt, s.UpdatedAt, s.CompletedAt, s.Version,
	)
}

func TestPaymentStatusRepository_GetByID(t *testing.T) {
	t.Run("returns the payment status", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		want := newTestPaymentStatus()
		mock.ExpectQuery("FROM payment_status WHERE id").WithArgs(want.ID).WillReturnRows(paymentStatusRow(want))

		repo := NewPaymentStatusRepository(db)
		got, err := repo.GetByID(context.Background(), want.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if got.ID != want.ID || got.Status != want.Status || got.AmountCents != want.AmountCents || got.TransactionID != want.TransactionID {
			t.Errorf("GetByID() = %+v, want %+v", got, want)
		}
		if got.CompletedAt == nil || !got.CompletedAt.Equal(*want.CompletedAt) {
			t.Errorf("CompletedAt = %v, want %v", got.CompletedAt, want.CompletedAt)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns not found when there is no row", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		id := uuid.New()
		mock.ExpectQuery("FROM payment_status WHERE id").WithArgs(id).WillReturnError(sql.ErrNoRows)

		repo := NewPaymentStatusRepository(db)
		_, err = repo.GetByID(context.Background(), id)
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "NOT_FOUND" {
			t.Errorf("error = %v, want NOT_FOUND", err)
		}
	})

	t.Run("returns error on query failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		id := uuid.New()
		mock.ExpectQuery("FROM payment_status WHERE id").WithArgs(id).WillReturnError(errors.New("boom"))

		repo := NewPaymentStatusRepository(db)
		if _, err := repo.GetByID(context.Background(), id); err == nil {
			t.Fatal("expected error, got none")
		}
	})

	t.Run("returns error when a row cannot be scanned", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		id := uuid.New()
		rows := sqlmock.NewRows(paymentStatusColumnNames).AddRow(
			"not-a-uuid", uuid.New(), uuid.New(), 100, "USD", "initiated", "", time.Now(), time.Now(), nil, 1)
		mock.ExpectQuery("FROM payment_status WHERE id").WithArgs(id).WillReturnRows(rows)

		repo := NewPaymentStatusRepository(db)
		if _, err := repo.GetByID(context.Background(), id); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestPaymentStatusRepository_ListByOrderID(t *testing.T) {
	t.Run("returns payments recorded for the order", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		want := newTestPaymentStatus()
		mock.ExpectQuery("FROM payment_status WHERE order_id").WithArgs(want.OrderID).WillReturnRows(paymentStatusRow(want))

		repo := NewPaymentStatusRepository(db)
		got, err := repo.ListByOrderID(context.Background(), want.OrderID)
		if err != nil {
			t.Fatalf("ListByOrderID() error = %v", err)
		}
		if len(got) != 1 || got[0].ID != want.ID {
			t.Errorf("ListByOrderID() = %+v, want [%+v]", got, want)
		}
	})

	t.Run("returns an empty slice when there are no payments", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		mock.ExpectQuery("FROM payment_status WHERE order_id").WithArgs(orderID).
			WillReturnRows(sqlmock.NewRows(paymentStatusColumnNames))

		repo := NewPaymentStatusRepository(db)
		got, err := repo.ListByOrderID(context.Background(), orderID)
		if err != nil {
			t.Fatalf("ListByOrderID() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("ListByOrderID() = %+v, want empty", got)
		}
	})

	t.Run("returns error on query failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		mock.ExpectQuery("FROM payment_status WHERE order_id").WithArgs(orderID).WillReturnError(errors.New("boom"))

		repo := NewPaymentStatusRepository(db)
		if _, err := repo.ListByOrderID(context.Background(), orderID); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}
