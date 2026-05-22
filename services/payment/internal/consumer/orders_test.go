package consumer

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

var errTestPaymentProcessor = errors.New("payment processor failure")

const processedEventsQuery = `SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)`

type paymentCall struct {
	orderID     uuid.UUID
	customerID  uuid.UUID
	amountCents int64
	currency    string
}

// fakePaymentProcessor is an in-memory PaymentProcessor test double.
type fakePaymentProcessor struct {
	calls []paymentCall
	err   error
}

func (f *fakePaymentProcessor) ProcessPayment(_ context.Context, orderID, customerID uuid.UUID, amountCents int64, currency string) (*domain.Payment, error) {
	f.calls = append(f.calls, paymentCall{orderID, customerID, amountCents, currency})
	if f.err != nil {
		return nil, f.err
	}
	return &domain.Payment{ID: uuid.New(), OrderID: orderID, CustomerID: customerID}, nil
}

// fakeSubscriber invokes handler once with a fixed event, exercising the real Start wiring.
type fakeSubscriber struct {
	event events.Event
}

func (f *fakeSubscriber) Subscribe(ctx context.Context, handler func(context.Context, events.Event) error) error {
	return handler(ctx, f.event)
}

func newOrderReadyEvent(orderID, customerID uuid.UUID, amountCents int64, currency string) events.Event {
	return events.Event{
		ID:   uuid.New().String(),
		Type: events.EventTypeOrderReadyForPayment,
		Data: map[string]interface{}{
			"order_id":           orderID.String(),
			"customer_id":        customerID.String(),
			"total_amount_cents": float64(amountCents),
			"currency":           currency,
		},
	}
}

func newConsumer(t *testing.T, db *sql.DB, payments PaymentProcessor) *OrdersConsumer {
	t.Helper()
	return &OrdersConsumer{
		db:        db,
		processed: events.NewProcessedStore(db),
		payments:  payments,
		logger:    zaptest.NewLogger(t),
	}
}

func expectWasProcessed(mock sqlmock.Sqlmock, eventID string, processed bool) {
	mock.ExpectQuery(regexp.QuoteMeta(processedEventsQuery)).
		WithArgs(eventID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(processed))
}

func TestOrdersConsumer_Handle(t *testing.T) {
	t.Run("charges the order for order.ready_for_payment", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID, customerID := uuid.New(), uuid.New()
		event := newOrderReadyEvent(orderID, customerID, 4999, "USD")

		expectWasProcessed(mock, event.ID, false)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		payments := &fakePaymentProcessor{}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(payments.calls) != 1 {
			t.Fatalf("calls = %d, want 1", len(payments.calls))
		}
		want := paymentCall{orderID: orderID, customerID: customerID, amountCents: 4999, currency: "USD"}
		if payments.calls[0] != want {
			t.Errorf("call = %+v, want %+v", payments.calls[0], want)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("marks a declined payment processed without erroring", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderReadyEvent(uuid.New(), uuid.New(), 4999, "USD")

		expectWasProcessed(mock, event.ID, false)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		payments := &fakePaymentProcessor{err: apperrors.NewPaymentFailed("insufficient_funds")}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(payments.calls) != 1 {
			t.Errorf("calls = %d, want 1", len(payments.calls))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("skips an already processed event without charging", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderReadyEvent(uuid.New(), uuid.New(), 4999, "USD")
		expectWasProcessed(mock, event.ID, true)

		payments := &fakePaymentProcessor{}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(payments.calls) != 0 {
			t.Errorf("calls = %d, want 0", len(payments.calls))
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

		event := newOrderReadyEvent(uuid.New(), uuid.New(), 4999, "USD")
		event.Type = events.EventTypeOrderConfirmed

		payments := &fakePaymentProcessor{}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(payments.calls) != 0 {
			t.Errorf("calls = %d, want 0", len(payments.calls))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("drops an event missing order_id", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := events.Event{ID: uuid.New().String(), Type: events.EventTypeOrderReadyForPayment, Data: map[string]interface{}{}}

		payments := &fakePaymentProcessor{}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(payments.calls) != 0 {
			t.Errorf("calls = %d, want 0", len(payments.calls))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("drops an event with a non-numeric amount", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderReadyEvent(uuid.New(), uuid.New(), 4999, "USD")
		event.Data["total_amount_cents"] = "not-a-number"

		payments := &fakePaymentProcessor{}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(payments.calls) != 0 {
			t.Errorf("calls = %d, want 0", len(payments.calls))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("propagates WasProcessed errors", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderReadyEvent(uuid.New(), uuid.New(), 4999, "USD")
		mock.ExpectQuery(regexp.QuoteMeta(processedEventsQuery)).
			WithArgs(event.ID).
			WillReturnError(errTestPaymentProcessor)

		payments := &fakePaymentProcessor{}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); err == nil {
			t.Fatal("expected error, got none")
		}
		if len(payments.calls) != 0 {
			t.Errorf("calls = %d, want 0", len(payments.calls))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns a technical processing error without marking the event processed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderReadyEvent(uuid.New(), uuid.New(), 4999, "USD")
		expectWasProcessed(mock, event.ID, false)

		payments := &fakePaymentProcessor{err: errTestPaymentProcessor}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); !errors.Is(err, errTestPaymentProcessor) {
			t.Errorf("error = %v, want %v", err, errTestPaymentProcessor)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("wraps a failure to begin the mark-processed transaction", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderReadyEvent(uuid.New(), uuid.New(), 4999, "USD")
		expectWasProcessed(mock, event.ID, false)
		mock.ExpectBegin().WillReturnError(errTestPaymentProcessor)

		payments := &fakePaymentProcessor{}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("wraps a failure to commit the mark-processed transaction", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderReadyEvent(uuid.New(), uuid.New(), 4999, "USD")
		expectWasProcessed(mock, event.ID, false)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit().WillReturnError(errTestPaymentProcessor)

		payments := &fakePaymentProcessor{}
		c := newConsumer(t, db, payments)

		if err := c.handle(context.Background(), event); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})
}

func TestOrdersConsumer_Start(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID, customerID := uuid.New(), uuid.New()
	event := newOrderReadyEvent(orderID, customerID, 4999, "USD")

	expectWasProcessed(mock, event.ID, false)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
		WithArgs(event.ID, event.Type).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	payments := &fakePaymentProcessor{}
	c := newConsumer(t, db, payments)
	c.subscriber = &fakeSubscriber{event: event}

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(payments.calls) != 1 {
		t.Errorf("calls = %d, want 1", len(payments.calls))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
