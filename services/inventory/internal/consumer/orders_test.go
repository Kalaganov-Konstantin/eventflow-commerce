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

var errTestStockService = errors.New("stock service failure")

type handleCall struct {
	eventType string
	orderID   uuid.UUID
}

// fakeStockService is an in-memory StockService test double.
type fakeStockService struct {
	calls []handleCall
	err   error
}

func (f *fakeStockService) HandleOrderEvent(_ context.Context, _ *sql.Tx, eventType string, orderID uuid.UUID) error {
	f.calls = append(f.calls, handleCall{eventType, orderID})
	return f.err
}

// fakeSubscriber invokes handler once with a fixed event, exercising the real Start wiring.
type fakeSubscriber struct {
	event events.Event
}

func (f *fakeSubscriber) Subscribe(ctx context.Context, handler func(context.Context, events.Event) error) error {
	return handler(ctx, f.event)
}

func newOrderEvent(eventType, orderID string) events.Event {
	return events.Event{
		ID:   uuid.New().String(),
		Type: eventType,
		Data: map[string]interface{}{"order_id": orderID},
	}
}

func newConsumer(t *testing.T, db *sql.DB, stock StockService) *OrdersConsumer {
	t.Helper()
	return &OrdersConsumer{
		db:        db,
		processed: events.NewProcessedStore(db),
		stock:     stock,
		logger:    zaptest.NewLogger(t),
	}
}

func TestNewOrdersConsumer(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	stock := &fakeStockService{}
	processed := events.NewProcessedStore(db)
	logger := zaptest.NewLogger(t)

	c := NewOrdersConsumer(nil, db, processed, stock, logger)

	if c.db != db {
		t.Errorf("db = %v, want %v", c.db, db)
	}
	if c.processed != processed {
		t.Errorf("processed = %v, want %v", c.processed, processed)
	}
	if c.stock != stock {
		t.Errorf("stock = %v, want %v", c.stock, stock)
	}
	if c.logger != logger {
		t.Errorf("logger = %v, want %v", c.logger, logger)
	}
}

func TestOrdersConsumer_Handle(t *testing.T) {
	t.Run("forwards order.cancelled to the stock service", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		event := newOrderEvent(events.EventTypeOrderCancelled, orderID.String())

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		stock := &fakeStockService{}
		c := newConsumer(t, db, stock)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(stock.calls) != 1 || stock.calls[0].eventType != events.EventTypeOrderCancelled || stock.calls[0].orderID != orderID {
			t.Errorf("calls = %v, want one call of order.cancelled for %v", stock.calls, orderID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("skips a redelivered event without touching the stock service", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderEvent(events.EventTypeOrderCancelled, uuid.New().String())

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		stock := &fakeStockService{}
		c := newConsumer(t, db, stock)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(stock.calls) != 0 {
			t.Errorf("calls = %v, want none", stock.calls)
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

		event := events.Event{ID: uuid.New().String(), Type: events.EventTypeOrderCancelled}

		stock := &fakeStockService{}
		c := newConsumer(t, db, stock)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(stock.calls) != 0 {
			t.Errorf("calls = %v, want none", stock.calls)
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

		event := newOrderEvent(events.EventTypeOrderCancelled, "not-a-uuid")

		stock := &fakeStockService{}
		c := newConsumer(t, db, stock)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(stock.calls) != 0 {
			t.Errorf("calls = %v, want none", stock.calls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("drops an event whose order id is not a string", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := events.Event{
			ID:   uuid.New().String(),
			Type: events.EventTypeOrderCancelled,
			Data: map[string]interface{}{"order_id": 12345},
		}

		stock := &fakeStockService{}
		c := newConsumer(t, db, stock)

		if err := c.handle(context.Background(), event); err != nil {
			t.Fatalf("handle() error = %v", err)
		}
		if len(stock.calls) != 0 {
			t.Errorf("calls = %v, want none", stock.calls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error when marking the event processed fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderEvent(events.EventTypeOrderCancelled, uuid.New().String())

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		stock := &fakeStockService{}
		c := newConsumer(t, db, stock)

		if err := c.handle(context.Background(), event); err == nil {
			t.Fatal("expected error, got none")
		}
		if len(stock.calls) != 0 {
			t.Errorf("calls = %v, want none", stock.calls)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error when the transaction fails to commit", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		event := newOrderEvent(events.EventTypeOrderCancelled, orderID.String())

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit().WillReturnError(errors.New("boom"))

		stock := &fakeStockService{}
		c := newConsumer(t, db, stock)

		if err := c.handle(context.Background(), event); err == nil {
			t.Fatal("expected error, got none")
		}
		if len(stock.calls) != 1 || stock.calls[0].orderID != orderID {
			t.Errorf("calls = %v, want one call for %v", stock.calls, orderID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("rolls back when the stock service fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		event := newOrderEvent(events.EventTypeOrderCancelled, uuid.New().String())

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
			WithArgs(event.ID, event.Type).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectRollback()

		stock := &fakeStockService{err: errTestStockService}
		c := newConsumer(t, db, stock)

		if err := c.handle(context.Background(), event); !errors.Is(err, errTestStockService) {
			t.Errorf("error = %v, want %v", err, errTestStockService)
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

		event := newOrderEvent(events.EventTypeOrderCancelled, uuid.New().String())
		mock.ExpectBegin().WillReturnError(errTestStockService)

		stock := &fakeStockService{}
		c := newConsumer(t, db, stock)

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

	orderID := uuid.New()
	event := newOrderEvent(events.EventTypeOrderCancelled, orderID.String())

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
		WithArgs(event.ID, event.Type).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	stock := &fakeStockService{}
	c := newConsumer(t, db, stock)
	c.subscriber = &fakeSubscriber{event: event}

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(stock.calls) != 1 || stock.calls[0].orderID != orderID {
		t.Errorf("calls = %v, want one call for %v", stock.calls, orderID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
