package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

var errTestRepository = errors.New("repository failure")

type updateCall struct {
	id              uuid.UUID
	status          domain.Status
	expectedVersion int
}

// fakeRepository is an in-memory Repository test double.
type fakeRepository struct {
	order  *domain.Order
	getErr error

	updateErr   error
	updateCalls []updateCall
}

func (f *fakeRepository) GetByID(_ context.Context, _ uuid.UUID) (*domain.Order, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	order := *f.order
	return &order, nil
}

func (f *fakeRepository) UpdateStatus(_ context.Context, _ *sql.Tx, id uuid.UUID, status domain.Status, expectedVersion int) error {
	f.updateCalls = append(f.updateCalls, updateCall{id, status, expectedVersion})
	return f.updateErr
}

func newTestOrder(status domain.Status) *domain.Order {
	return &domain.Order{
		ID:               uuid.New(),
		CustomerID:       uuid.New(),
		Status:           status,
		TotalAmountCents: 1998,
		Currency:         "USD",
		Version:          1,
	}
}

func TestOrderService_MarkPendingPayment(t *testing.T) {
	t.Run("transitions to pending_payment and enqueues the event", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPending)
		repo := &fakeRepository{order: order}
		svc := NewOrderService(repo)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).
			WithArgs(sqlmock.AnyArg(), "orders.events", "order.ready_for_payment", order.ID.String(), sqlmock.AnyArg(), nil).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.MarkPendingPayment(context.Background(), tx, order.ID); err != nil {
			t.Fatalf("MarkPendingPayment() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("tx.Commit() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}

		if len(repo.updateCalls) != 1 {
			t.Fatalf("UpdateStatus calls = %d, want 1", len(repo.updateCalls))
		}
		if call := repo.updateCalls[0]; call.status != domain.StatusPendingPayment || call.expectedVersion != 1 {
			t.Errorf("UpdateStatus call = %+v, want status=%v expectedVersion=1", call, domain.StatusPendingPayment)
		}
	})

	t.Run("rejects an order in the wrong status without touching the outbox", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusConfirmed)
		repo := &fakeRepository{order: order}
		svc := NewOrderService(repo)

		mock.ExpectBegin()
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		err = svc.MarkPendingPayment(context.Background(), tx, order.ID)
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "ORDER_ALREADY_PROCESSED" {
			t.Errorf("error = %v, want ORDER_ALREADY_PROCESSED", err)
		}
		if len(repo.updateCalls) != 0 {
			t.Errorf("UpdateStatus calls = %d, want 0", len(repo.updateCalls))
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("propagates a repository read failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		repo := &fakeRepository{getErr: errTestRepository}
		svc := NewOrderService(repo)

		mock.ExpectBegin()
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.MarkPendingPayment(context.Background(), tx, uuid.New()); !errors.Is(err, errTestRepository) {
			t.Errorf("error = %v, want %v", err, errTestRepository)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})
}

func TestOrderService_ConfirmPayment(t *testing.T) {
	t.Run("transitions to confirmed and enqueues order.confirmed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPendingPayment)
		repo := &fakeRepository{order: order}
		svc := NewOrderService(repo)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).
			WithArgs(sqlmock.AnyArg(), "orders.events", "order.confirmed", order.ID.String(), sqlmock.AnyArg(), nil).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.ConfirmPayment(context.Background(), tx, order.ID); err != nil {
			t.Fatalf("ConfirmPayment() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("tx.Commit() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("propagates an outbox insert failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPendingPayment)
		repo := &fakeRepository{order: order}
		svc := NewOrderService(repo)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).WillReturnError(errTestRepository)
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.ConfirmPayment(context.Background(), tx, order.ID); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("ignores an order that already reached a terminal status", func(t *testing.T) {
		for _, status := range []domain.Status{domain.StatusConfirmed, domain.StatusPaymentFailed, domain.StatusCancelled} {
			t.Run(string(status), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatalf("sqlmock.New() error = %v", err)
				}
				defer func() { _ = db.Close() }()

				order := newTestOrder(status)
				repo := &fakeRepository{order: order}
				svc := NewOrderService(repo)

				mock.ExpectBegin()
				mock.ExpectCommit()

				tx, err := db.Begin()
				if err != nil {
					t.Fatalf("db.Begin() error = %v", err)
				}

				if err := svc.ConfirmPayment(context.Background(), tx, order.ID); err != nil {
					t.Fatalf("ConfirmPayment() error = %v", err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatalf("tx.Commit() error = %v", err)
				}
				if len(repo.updateCalls) != 0 {
					t.Errorf("UpdateStatus calls = %v, want none", repo.updateCalls)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("unmet expectations: %v", err)
				}
			})
		}
	})
}

func TestOrderService_FailPayment(t *testing.T) {
	t.Run("transitions to payment_failed and enqueues order.cancelled", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPendingPayment)
		repo := &fakeRepository{order: order}
		svc := NewOrderService(repo)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).
			WithArgs(sqlmock.AnyArg(), "orders.events", "order.cancelled", order.ID.String(), sqlmock.AnyArg(), nil).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.FailPayment(context.Background(), tx, order.ID); err != nil {
			t.Fatalf("FailPayment() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("tx.Commit() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}

		if len(repo.updateCalls) != 1 || repo.updateCalls[0].status != domain.StatusPaymentFailed {
			t.Errorf("UpdateStatus calls = %+v, want a single call to payment_failed", repo.updateCalls)
		}
	})

	t.Run("propagates a version conflict from the repository", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPendingPayment)
		repo := &fakeRepository{order: order, updateErr: apperrors.NewConflict("stale version")}
		svc := NewOrderService(repo)

		mock.ExpectBegin()
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		err = svc.FailPayment(context.Background(), tx, order.ID)
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "CONFLICT" {
			t.Errorf("error = %v, want CONFLICT", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("ignores an order that already reached a terminal status", func(t *testing.T) {
		for _, status := range []domain.Status{domain.StatusConfirmed, domain.StatusPaymentFailed, domain.StatusCancelled} {
			t.Run(string(status), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatalf("sqlmock.New() error = %v", err)
				}
				defer func() { _ = db.Close() }()

				order := newTestOrder(status)
				repo := &fakeRepository{order: order}
				svc := NewOrderService(repo)

				mock.ExpectBegin()
				mock.ExpectCommit()

				tx, err := db.Begin()
				if err != nil {
					t.Fatalf("db.Begin() error = %v", err)
				}

				if err := svc.FailPayment(context.Background(), tx, order.ID); err != nil {
					t.Fatalf("FailPayment() error = %v", err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatalf("tx.Commit() error = %v", err)
				}
				if len(repo.updateCalls) != 0 {
					t.Errorf("UpdateStatus calls = %v, want none", repo.updateCalls)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("unmet expectations: %v", err)
				}
			})
		}
	})
}
