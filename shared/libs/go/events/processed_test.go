package events

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestProcessedStore_MarkProcessed_ReturnsTrueForNewEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
		WithArgs("evt-1", "order.created").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	store := NewProcessedStore(db)
	processed, err := store.MarkProcessed(context.Background(), tx, "evt-1", "order.created")
	if err != nil {
		t.Fatalf("MarkProcessed() error = %v", err)
	}
	if !processed {
		t.Error("MarkProcessed() = false, want true for a new event")
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestProcessedStore_MarkProcessed_ReturnsFalseForRedelivery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
		WithArgs("evt-1", "order.created").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	store := NewProcessedStore(db)
	processed, err := store.MarkProcessed(context.Background(), tx, "evt-1", "order.created")
	if err != nil {
		t.Fatalf("MarkProcessed() error = %v", err)
	}
	if processed {
		t.Error("MarkProcessed() = true, want false for a redelivered event")
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestProcessedStore_MarkProcessed_WrapsExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO processed_events")).
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	store := NewProcessedStore(db)
	if _, err := store.MarkProcessed(context.Background(), tx, "evt-1", "order.created"); err == nil {
		t.Fatal("MarkProcessed() error = nil, want error")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestProcessedStore_WasProcessed(t *testing.T) {
	tests := []struct {
		name   string
		exists bool
	}{
		{"already processed", true},
		{"not processed", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer db.Close()

			mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS")).
				WithArgs("evt-1").
				WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tt.exists))

			store := NewProcessedStore(db)
			got, err := store.WasProcessed(context.Background(), "evt-1")
			if err != nil {
				t.Fatalf("WasProcessed() error = %v", err)
			}
			if got != tt.exists {
				t.Errorf("WasProcessed() = %v, want %v", got, tt.exists)
			}
		})
	}
}

func TestIdempotent_SkipsAlreadyProcessedEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS")).
		WithArgs("evt-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	store := NewProcessedStore(db)
	called := false
	handler := Idempotent(store, func(Event) error {
		called = true
		return nil
	})

	if err := handler(Event{ID: "evt-1"}); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if called {
		t.Error("handler was called for an already processed event")
	}
}

func TestIdempotent_CallsHandlerForNewEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS")).
		WithArgs("evt-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	store := NewProcessedStore(db)
	called := false
	handler := Idempotent(store, func(Event) error {
		called = true
		return nil
	})

	if err := handler(Event{ID: "evt-1"}); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if !called {
		t.Error("handler was not called for a new event")
	}
}
