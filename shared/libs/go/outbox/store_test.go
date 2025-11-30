package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestStore_Enqueue_InsertsMessageWithinTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	payload := json.RawMessage(`{"total_cents":1999}`)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).
		WithArgs(sqlmock.AnyArg(), "orders.events", "order.created", "order-1", []byte(payload), nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	store := NewStore()
	if err := store.Enqueue(context.Background(), tx, Message{
		Topic:       "orders.events",
		EventType:   "order.created",
		AggregateID: "order-1",
		Payload:     payload,
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStore_Enqueue_SetsCorrelationIDWhenProvided(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	payload := json.RawMessage(`{}`)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).
		WithArgs(sqlmock.AnyArg(), "payments.events", "payment.initiated", "payment-1", []byte(payload), "corr-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	store := NewStore()
	if err := store.Enqueue(context.Background(), tx, Message{
		Topic:         "payments.events",
		EventType:     "payment.initiated",
		AggregateID:   "payment-1",
		Payload:       payload,
		CorrelationID: "corr-1",
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStore_Enqueue_WrapsExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	store := NewStore()
	err = store.Enqueue(context.Background(), tx, Message{
		Topic:       "orders.events",
		EventType:   "order.created",
		AggregateID: "order-1",
		Payload:     json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("Enqueue() error = nil, want error")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
