package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/saga"
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
	order    *domain.Order
	getErr   error
	getCalls int

	saveErr   error
	saveCalls []*domain.Order

	listResult []*domain.Order
	listErr    error

	updateErr   error
	updateCalls []updateCall
}

func (f *fakeRepository) Save(_ context.Context, order *domain.Order) error {
	f.saveCalls = append(f.saveCalls, order)
	return f.saveErr
}

func (f *fakeRepository) GetByID(_ context.Context, _ uuid.UUID) (*domain.Order, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	order := *f.order
	return &order, nil
}

func (f *fakeRepository) ListByCustomer(_ context.Context, _ uuid.UUID, _, _ int) ([]*domain.Order, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeRepository) UpdateStatus(_ context.Context, _ *sql.Tx, id uuid.UUID, status domain.Status, expectedVersion int) error {
	f.updateCalls = append(f.updateCalls, updateCall{id, status, expectedVersion})
	return f.updateErr
}

type sagaTransitionCall struct {
	orderID uuid.UUID
	to      saga.State
}

// fakeSagaRepository is an in-memory SagaRepository test double.
type fakeSagaRepository struct {
	startErr   error
	startCalls []uuid.UUID

	transitionErr   map[saga.State]error
	transitionCalls []sagaTransitionCall

	lastErrorCalls []string
}

func (f *fakeSagaRepository) Start(_ context.Context, _ *sql.Tx, orderID uuid.UUID) error {
	f.startCalls = append(f.startCalls, orderID)
	return f.startErr
}

func (f *fakeSagaRepository) Transition(_ context.Context, _ *sql.Tx, orderID uuid.UUID, to saga.State) error {
	f.transitionCalls = append(f.transitionCalls, sagaTransitionCall{orderID, to})
	if f.transitionErr != nil {
		if err, ok := f.transitionErr[to]; ok {
			return err
		}
	}
	return nil
}

func (f *fakeSagaRepository) SetLastError(_ context.Context, _ uuid.UUID, message string) error {
	f.lastErrorCalls = append(f.lastErrorCalls, message)
	return nil
}

// fakeInventoryReleaser is an in-memory InventoryReleaser test double.
type fakeInventoryReleaser struct {
	releaseErr error
	released   []uuid.UUID
}

func (f *fakeInventoryReleaser) Release(_ context.Context, orderID uuid.UUID) error {
	f.released = append(f.released, orderID)
	return f.releaseErr
}

type paymentRefundCall struct {
	paymentID uuid.UUID
	reason    string
}

// fakePaymentRefunder is an in-memory PaymentRefunder test double.
type fakePaymentRefunder struct {
	refundErr   error
	refundCalls []paymentRefundCall
}

func (f *fakePaymentRefunder) Refund(_ context.Context, paymentID uuid.UUID, reason string) error {
	f.refundCalls = append(f.refundCalls, paymentRefundCall{paymentID, reason})
	return f.refundErr
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
		sagaRepo := &fakeSagaRepository{}
		svc := NewOrderService(repo, db, sagaRepo, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

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
		if len(sagaRepo.startCalls) != 1 || sagaRepo.startCalls[0] != order.ID {
			t.Errorf("saga start calls = %v, want a single start of %v", sagaRepo.startCalls, order.ID)
		}
		wantTransitions := []sagaTransitionCall{
			{order.ID, saga.StateStockReserved},
			{order.ID, saga.StateAwaitingPayment},
		}
		if !reflect.DeepEqual(sagaRepo.transitionCalls, wantTransitions) {
			t.Errorf("saga transitions = %+v, want %+v", sagaRepo.transitionCalls, wantTransitions)
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
		svc := NewOrderService(repo, db, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

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
		svc := NewOrderService(repo, db, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

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

func TestOrderService_MarkPendingPaymentAfterCreate(t *testing.T) {
	t.Run("commits its own transaction", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPending)
		repo := &fakeRepository{order: order}
		svc := NewOrderService(repo, db, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).
			WithArgs(sqlmock.AnyArg(), "orders.events", "order.ready_for_payment", order.ID.String(), sqlmock.AnyArg(), nil).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		if err := svc.MarkPendingPaymentAfterCreate(context.Background(), order.ID); err != nil {
			t.Fatalf("MarkPendingPaymentAfterCreate() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
		if len(repo.updateCalls) != 1 || repo.updateCalls[0].status != domain.StatusPendingPayment {
			t.Errorf("UpdateStatus calls = %+v, want a single call to pending_payment", repo.updateCalls)
		}
	})

	t.Run("propagates a begin transaction failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		repo := &fakeRepository{order: newTestOrder(domain.StatusPending)}
		svc := NewOrderService(repo, db, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

		mock.ExpectBegin().WillReturnError(errTestRepository)

		if err := svc.MarkPendingPaymentAfterCreate(context.Background(), uuid.New()); !errors.Is(err, errTestRepository) {
			t.Errorf("error = %v, want %v", err, errTestRepository)
		}
	})

	t.Run("rolls back on an invalid transition", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusConfirmed)
		repo := &fakeRepository{order: order}
		svc := NewOrderService(repo, db, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

		mock.ExpectBegin()
		mock.ExpectRollback()

		err = svc.MarkPendingPaymentAfterCreate(context.Background(), order.ID)
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "ORDER_ALREADY_PROCESSED" {
			t.Errorf("error = %v, want ORDER_ALREADY_PROCESSED", err)
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
		sagaRepo := &fakeSagaRepository{}
		svc := NewOrderService(repo, db, sagaRepo, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

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

		wantTransitions := []sagaTransitionCall{
			{order.ID, saga.StatePaid},
			{order.ID, saga.StateCompleted},
		}
		if !reflect.DeepEqual(sagaRepo.transitionCalls, wantTransitions) {
			t.Errorf("saga transitions = %+v, want %+v", sagaRepo.transitionCalls, wantTransitions)
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
		svc := NewOrderService(repo, db, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

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
				svc := NewOrderService(repo, db, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

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
	t.Run("compensates and transitions to payment_failed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPendingPayment)
		repo := &fakeRepository{order: order}
		sagaRepo := &fakeSagaRepository{}
		inventory := &fakeInventoryReleaser{}
		svc := NewOrderService(repo, db, sagaRepo, inventory, &fakePaymentRefunder{})

		mock.ExpectBegin() // the caller's transaction, opened below
		mock.ExpectBegin() // markCompensating's own transaction
		mock.ExpectCommit()
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
		if len(inventory.released) != 1 || inventory.released[0] != order.ID {
			t.Errorf("released = %v, want a single release of %v", inventory.released, order.ID)
		}
		wantTransitions := []sagaTransitionCall{
			{order.ID, saga.StateCompensating},
			{order.ID, saga.StateCompensated},
		}
		if !reflect.DeepEqual(sagaRepo.transitionCalls, wantTransitions) {
			t.Errorf("saga transitions = %+v, want %+v", sagaRepo.transitionCalls, wantTransitions)
		}
	})

	t.Run("a release failure leaves the saga compensating and returns an error", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPendingPayment)
		repo := &fakeRepository{order: order}
		sagaRepo := &fakeSagaRepository{}
		releaseErr := errors.New("inventory service unavailable")
		inventory := &fakeInventoryReleaser{releaseErr: releaseErr}
		svc := NewOrderService(repo, db, sagaRepo, inventory, &fakePaymentRefunder{})

		mock.ExpectBegin() // the caller's transaction, opened below and never used
		mock.ExpectBegin() // markCompensating's own transaction
		mock.ExpectCommit()
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		err = svc.FailPayment(context.Background(), tx, order.ID)
		if !errors.Is(err, releaseErr) {
			t.Errorf("error = %v, want to wrap %v", err, releaseErr)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}

		if len(repo.updateCalls) != 0 {
			t.Errorf("UpdateStatus calls = %v, want none", repo.updateCalls)
		}
		wantTransitions := []sagaTransitionCall{{order.ID, saga.StateCompensating}}
		if !reflect.DeepEqual(sagaRepo.transitionCalls, wantTransitions) {
			t.Errorf("saga transitions = %+v, want the saga left compensating", sagaRepo.transitionCalls)
		}
		if len(sagaRepo.lastErrorCalls) != 1 {
			t.Errorf("expected the failure to be recorded on the saga, lastErrorCalls = %v", sagaRepo.lastErrorCalls)
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
		svc := NewOrderService(repo, db, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

		mock.ExpectBegin() // the caller's transaction, opened below
		mock.ExpectBegin() // markCompensating's own transaction
		mock.ExpectCommit()
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
				sagaRepo := &fakeSagaRepository{}
				svc := NewOrderService(repo, db, sagaRepo, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

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
				if len(sagaRepo.transitionCalls) != 0 {
					t.Errorf("saga transitions = %v, want none", sagaRepo.transitionCalls)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("unmet expectations: %v", err)
				}
			})
		}
	})
}

func TestOrderService_FailAfterPayment(t *testing.T) {
	reason := "downstream_failure"

	t.Run("refunds, releases and cancels in the reverse order of the saga's steps", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPendingPayment)
		paymentID := uuid.New()
		repo := &fakeRepository{order: order}
		sagaRepo := &fakeSagaRepository{}
		inventory := &fakeInventoryReleaser{}
		payments := &fakePaymentRefunder{}
		svc := NewOrderService(repo, db, sagaRepo, inventory, payments)

		mock.ExpectBegin() // the caller's transaction, opened below
		mock.ExpectBegin() // markCompensating's own transaction
		mock.ExpectCommit()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).
			WithArgs(sqlmock.AnyArg(), "orders.events", "order.cancelled", order.ID.String(), sqlmock.AnyArg(), nil).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.FailAfterPayment(context.Background(), tx, order.ID, paymentID, reason); err != nil {
			t.Fatalf("FailAfterPayment() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("tx.Commit() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}

		if len(repo.updateCalls) != 1 || repo.updateCalls[0].status != domain.StatusCancelled {
			t.Errorf("UpdateStatus calls = %+v, want a single call to cancelled", repo.updateCalls)
		}
		if len(payments.refundCalls) != 1 || payments.refundCalls[0] != (paymentRefundCall{paymentID, reason}) {
			t.Errorf("refund calls = %+v, want a single refund of %v", payments.refundCalls, paymentID)
		}
		if len(inventory.released) != 1 || inventory.released[0] != order.ID {
			t.Errorf("released = %v, want a single release of %v", inventory.released, order.ID)
		}
		wantTransitions := []sagaTransitionCall{
			{order.ID, saga.StateCompensating},
			{order.ID, saga.StateCompensated},
		}
		if !reflect.DeepEqual(sagaRepo.transitionCalls, wantTransitions) {
			t.Errorf("saga transitions = %+v, want %+v", sagaRepo.transitionCalls, wantTransitions)
		}
	})

	t.Run("a refund failure leaves the saga compensating without releasing stock", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPendingPayment)
		refundErr := errors.New("payment service unavailable")
		repo := &fakeRepository{order: order}
		sagaRepo := &fakeSagaRepository{}
		inventory := &fakeInventoryReleaser{}
		payments := &fakePaymentRefunder{refundErr: refundErr}
		svc := NewOrderService(repo, db, sagaRepo, inventory, payments)

		mock.ExpectBegin() // the caller's transaction, opened below and never used
		mock.ExpectBegin() // markCompensating's own transaction
		mock.ExpectCommit()
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		err = svc.FailAfterPayment(context.Background(), tx, order.ID, uuid.New(), reason)
		if !errors.Is(err, refundErr) {
			t.Errorf("error = %v, want to wrap %v", err, refundErr)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}

		if len(inventory.released) != 0 {
			t.Errorf("released = %v, want no release attempt before a successful refund", inventory.released)
		}
		wantTransitions := []sagaTransitionCall{{order.ID, saga.StateCompensating}}
		if !reflect.DeepEqual(sagaRepo.transitionCalls, wantTransitions) {
			t.Errorf("saga transitions = %+v, want the saga left compensating", sagaRepo.transitionCalls)
		}
		if len(sagaRepo.lastErrorCalls) != 1 {
			t.Errorf("expected the failure to be recorded on the saga, lastErrorCalls = %v", sagaRepo.lastErrorCalls)
		}
	})

	t.Run("a release failure after a successful refund leaves the saga compensating", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder(domain.StatusPendingPayment)
		releaseErr := errors.New("inventory service unavailable")
		repo := &fakeRepository{order: order}
		sagaRepo := &fakeSagaRepository{}
		inventory := &fakeInventoryReleaser{releaseErr: releaseErr}
		payments := &fakePaymentRefunder{}
		svc := NewOrderService(repo, db, sagaRepo, inventory, payments)

		mock.ExpectBegin() // the caller's transaction, opened below and never used
		mock.ExpectBegin() // markCompensating's own transaction
		mock.ExpectCommit()
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		err = svc.FailAfterPayment(context.Background(), tx, order.ID, uuid.New(), reason)
		if !errors.Is(err, releaseErr) {
			t.Errorf("error = %v, want to wrap %v", err, releaseErr)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}

		if len(payments.refundCalls) != 1 {
			t.Errorf("refund calls = %v, want a single refund attempt", payments.refundCalls)
		}
		wantTransitions := []sagaTransitionCall{{order.ID, saga.StateCompensating}}
		if !reflect.DeepEqual(sagaRepo.transitionCalls, wantTransitions) {
			t.Errorf("saga transitions = %+v, want the saga left compensating", sagaRepo.transitionCalls)
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
				payments := &fakePaymentRefunder{}
				inventory := &fakeInventoryReleaser{}
				svc := NewOrderService(repo, db, &fakeSagaRepository{}, inventory, payments)

				mock.ExpectBegin()
				mock.ExpectCommit()

				tx, err := db.Begin()
				if err != nil {
					t.Fatalf("db.Begin() error = %v", err)
				}

				if err := svc.FailAfterPayment(context.Background(), tx, order.ID, uuid.New(), reason); err != nil {
					t.Fatalf("FailAfterPayment() error = %v", err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatalf("tx.Commit() error = %v", err)
				}
				if len(payments.refundCalls) != 0 || len(inventory.released) != 0 {
					t.Errorf("expected no compensation, refundCalls = %v released = %v", payments.refundCalls, inventory.released)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("unmet expectations: %v", err)
				}
			})
		}
	})
}

// fakeOrderCache is an in-memory OrderCache test double.
type fakeOrderCache struct {
	store  map[string][]byte
	getErr error
	setErr error
}

func newFakeOrderCache() *fakeOrderCache {
	return &fakeOrderCache{store: make(map[string][]byte)}
}

func (f *fakeOrderCache) GetJSON(_ context.Context, key string, dest any) (bool, error) {
	if f.getErr != nil {
		return false, f.getErr
	}
	raw, ok := f.store[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, dest)
}

func (f *fakeOrderCache) SetJSON(_ context.Context, key string, value any, _ time.Duration) error {
	if f.setErr != nil {
		return f.setErr
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f.store[key] = data
	return nil
}

func TestOrderService_GetByID_SecondReadDoesNotHitRepository(t *testing.T) {
	order := newTestOrder(domain.StatusPendingPayment)
	repo := &fakeRepository{order: order}
	svc := NewOrderService(repo, nil, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})
	svc.SetCache(newFakeOrderCache())

	first, err := svc.GetByID(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("GetByID() first call error = %v", err)
	}
	if first.ID != order.ID {
		t.Errorf("GetByID() first call ID = %v, want %v", first.ID, order.ID)
	}
	if repo.getCalls != 1 {
		t.Fatalf("repository GetByID calls after first read = %d, want 1", repo.getCalls)
	}

	second, err := svc.GetByID(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("GetByID() second call error = %v", err)
	}
	if second.ID != order.ID {
		t.Errorf("GetByID() second call ID = %v, want %v", second.ID, order.ID)
	}
	if repo.getCalls != 1 {
		t.Errorf("repository GetByID calls after second read = %d, want 1 (should be served from cache)", repo.getCalls)
	}
}

func TestOrderService_GetByID_NoCacheAlwaysReadsRepository(t *testing.T) {
	order := newTestOrder(domain.StatusPendingPayment)
	repo := &fakeRepository{order: order}
	svc := NewOrderService(repo, nil, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

	if _, err := svc.GetByID(context.Background(), order.ID); err != nil {
		t.Fatalf("GetByID() first call error = %v", err)
	}
	if _, err := svc.GetByID(context.Background(), order.ID); err != nil {
		t.Fatalf("GetByID() second call error = %v", err)
	}

	if repo.getCalls != 2 {
		t.Errorf("repository GetByID calls = %d, want 2 (no cache configured)", repo.getCalls)
	}
}

func TestOrderService_GetByID_CacheErrorFallsBackToRepository(t *testing.T) {
	order := newTestOrder(domain.StatusPendingPayment)
	repo := &fakeRepository{order: order}
	cache := newFakeOrderCache()
	cache.getErr = errors.New("redis unavailable")
	svc := NewOrderService(repo, nil, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})
	svc.SetCache(cache)

	got, err := svc.GetByID(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v, want nil", err)
	}
	if got.ID != order.ID {
		t.Errorf("GetByID() ID = %v, want %v", got.ID, order.ID)
	}
	if repo.getCalls != 1 {
		t.Errorf("repository GetByID calls = %d, want 1", repo.getCalls)
	}
}

func TestOrderService_GetByID_RepositoryError(t *testing.T) {
	repo := &fakeRepository{getErr: errTestRepository}
	svc := NewOrderService(repo, nil, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})
	svc.SetCache(newFakeOrderCache())

	if _, err := svc.GetByID(context.Background(), uuid.New()); !errors.Is(err, errTestRepository) {
		t.Errorf("GetByID() error = %v, want %v", err, errTestRepository)
	}
}

func TestOrderService_Save_DelegatesToRepository(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewOrderService(repo, nil, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

	order := newTestOrder(domain.StatusPendingPayment)
	if err := svc.Save(context.Background(), order); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if len(repo.saveCalls) != 1 || repo.saveCalls[0] != order {
		t.Errorf("saveCalls = %v, want one call with %v", repo.saveCalls, order)
	}
}

func TestOrderService_ListByCustomer_DelegatesToRepository(t *testing.T) {
	want := []*domain.Order{newTestOrder(domain.StatusPendingPayment)}
	repo := &fakeRepository{listResult: want}
	svc := NewOrderService(repo, nil, &fakeSagaRepository{}, &fakeInventoryReleaser{}, &fakePaymentRefunder{})

	got, err := svc.ListByCustomer(context.Background(), uuid.New(), 10, 0)
	if err != nil {
		t.Fatalf("ListByCustomer() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("ListByCustomer() returned %d orders, want %d", len(got), len(want))
	}
}
