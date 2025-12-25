package consumer

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

var errTestOrderService = errors.New("order service failure")

// fakeOrderService is an in-memory OrderService test double.
type fakeOrderService struct {
	confirmCalls []uuid.UUID
	failCalls    []uuid.UUID
	confirmErr   error
	failErr      error
}

func (f *fakeOrderService) ConfirmPayment(_ context.Context, _ *sql.Tx, orderID uuid.UUID) error {
	f.confirmCalls = append(f.confirmCalls, orderID)
	return f.confirmErr
}

func (f *fakeOrderService) FailPayment(_ context.Context, _ *sql.Tx, orderID uuid.UUID) error {
	f.failCalls = append(f.failCalls, orderID)
	return f.failErr
}

// fakeSubscriber invokes handler once with a fixed event, exercising the real Start wiring.
type fakeSubscriber struct {
	event events.Event
}

func (f *fakeSubscriber) Subscribe(_ context.Context, handler func(events.Event) error) error {
	return handler(f.event)
}

func newPaymentEvent(eventType, orderID string) events.Event {
	return events.Event{
		ID:   uuid.New().String(),
		Type: eventType,
		Data: map[string]interface{}{"order_id": orderID},
	}
}

func newConsumer(t *testing.T, db *sql.DB, orders OrderService) *PaymentsConsumer {
	t.Helper()
	return &PaymentsConsumer{
		db:        db,
		processed: events.NewProcessedStore(db),
		orders:    orders,
		logger:    zaptest.NewLogger(t),
	}
}

func TestPaymentsConsumer_Handle(t *testing.T) {
	t.Run("confirms the order on payment.processed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		event := newPaymentEvent(events.EventTypePaymentProcessed, orderID.String())

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		orders := &fakeOrderService{}
		c := newConsumer(t, db, orders)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(orders.confirmCalls) != 1 || orders.confirmCalls[0] != orderID {
			t.Errorf("confirmCalls = %v, want [%v]", orders.confirmCalls, orderID)
		}
		if len(orders.failCalls) != 0 {
			t.Errorf("failCalls = %v, want none", orders.failCalls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("fails the order on payment.failed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		event := newPaymentEvent(events.EventTypePaymentFailed, orderID.String())

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		orders := &fakeOrderService{}
		c := newConsumer(t, db, orders)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(orders.failCalls) != 1 || orders.failCalls[0] != orderID {
			t.Errorf("failCalls = %v, want [%v]", orders.failCalls, orderID)
		}
		if len(orders.confirmCalls) != 0 {
			t.Errorf("confirmCalls = %v, want none", orders.confirmCalls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("skips a redelivered event without touching the order", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newPaymentEvent(events.EventTypePaymentProcessed, uuid.New().String())

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		orders := &fakeOrderService{}
		c := newConsumer(t, db, orders)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(orders.confirmCalls) != 0 {
			t.Errorf("confirmCalls = %v, want none", orders.confirmCalls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("skips an unknown event type without touching the database", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newPaymentEvent(events.EventTypePaymentRefunded, uuid.New().String())

		orders := &fakeOrderService{}
		c := newConsumer(t, db, orders)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(orders.confirmCalls) != 0 || len(orders.failCalls) != 0 {
			t.Errorf("expected no order service calls, got confirm=%v fail=%v", orders.confirmCalls, orders.failCalls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("drops an event with a missing order id", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := events.Event{ID: uuid.New().String(), Type: events.EventTypePaymentProcessed}

		orders := &fakeOrderService{}
		c := newConsumer(t, db, orders)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(orders.confirmCalls) != 0 {
			t.Errorf("confirmCalls = %v, want none", orders.confirmCalls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("drops an event with an invalid order id", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newPaymentEvent(events.EventTypePaymentProcessed, "not-a-uuid")

		orders := &fakeOrderService{}
		c := newConsumer(t, db, orders)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(orders.confirmCalls) != 0 {
			t.Errorf("confirmCalls = %v, want none", orders.confirmCalls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("rolls back when the order service fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newPaymentEvent(events.EventTypePaymentProcessed, uuid.New().String())

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectRollback()

		orders := &fakeOrderService{confirmErr: errTestOrderService}
		c := newConsumer(t, db, orders)

		if err := c.handle(context.Background(), event); !errors.Is(err, errTestOrderService) {
			t.Errorf("error = %v, want %v", err, errTestOrderService)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("wraps a failure to begin the transaction", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newPaymentEvent(events.EventTypePaymentProcessed, uuid.New().String())
		mock.ExpectBegin().WillReturnError(errTestOrderService)

		orders := &fakeOrderService{}
		c := newConsumer(t, db, orders)

		if err := c.handle(context.Background(), event); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})
}

func TestPaymentsConsumer_Start(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID := uuid.New()
	event := newPaymentEvent(events.EventTypePaymentProcessed, orderID.String())

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
		WithArgs(event.ID, event.Type).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	orders := &fakeOrderService{}
	c := newConsumer(t, db, orders)
	c.subscriber = &fakeSubscriber{event: event}

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(orders.confirmCalls) != 1 || orders.confirmCalls[0] != orderID {
		t.Errorf("confirmCalls = %v, want [%v]", orders.confirmCalls, orderID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
