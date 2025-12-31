package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/google/uuid"
)

var errTestRepository = errors.New("repository failure")

// fakeRepository is an in-memory Repository test double.
type fakeRepository struct {
	releaseItems []domain.ReserveItem
	releaseErr   error
	releaseCalls []uuid.UUID
}

func (f *fakeRepository) ReleaseInTx(_ context.Context, _ *sql.Tx, orderID uuid.UUID) ([]domain.ReserveItem, error) {
	f.releaseCalls = append(f.releaseCalls, orderID)
	return f.releaseItems, f.releaseErr
}

func TestStockService_HandleOrderEvent(t *testing.T) {
	t.Run("releases reservations and enqueues inventory.released on order.cancelled", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		items := []domain.ReserveItem{{ProductID: uuid.New(), Quantity: 2}}
		repo := &fakeRepository{releaseItems: items}
		svc := NewStockService(repo)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).
			WithArgs(sqlmock.AnyArg(), "inventory.events", "inventory.released", orderID.String(), sqlmock.AnyArg(), nil).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.HandleOrderEvent(context.Background(), tx, events.EventTypeOrderCancelled, orderID); err != nil {
			t.Fatalf("HandleOrderEvent() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("tx.Commit() error = %v", err)
		}
		if len(repo.releaseCalls) != 1 || repo.releaseCalls[0] != orderID {
			t.Errorf("releaseCalls = %v, want [%v]", repo.releaseCalls, orderID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("is a no-op when there is nothing to release", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		repo := &fakeRepository{}
		svc := NewStockService(repo)

		mock.ExpectBegin()
		mock.ExpectCommit()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.HandleOrderEvent(context.Background(), tx, events.EventTypeOrderCancelled, uuid.New()); err != nil {
			t.Fatalf("HandleOrderEvent() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("tx.Commit() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("ignores event types it does not act on", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := NewStockService(repo)

		if err := svc.HandleOrderEvent(context.Background(), nil, events.EventTypeOrderCreated, uuid.New()); err != nil {
			t.Fatalf("HandleOrderEvent() error = %v", err)
		}
		if len(repo.releaseCalls) != 0 {
			t.Errorf("releaseCalls = %v, want none", repo.releaseCalls)
		}
	})

	t.Run("propagates a repository failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New() error = %v", err)
		}
		defer func() { _ = db.Close() }()

		repo := &fakeRepository{releaseErr: errTestRepository}
		svc := NewStockService(repo)

		mock.ExpectBegin()
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.HandleOrderEvent(context.Background(), tx, events.EventTypeOrderCancelled, uuid.New()); !errors.Is(err, errTestRepository) {
			t.Errorf("error = %v, want %v", err, errTestRepository)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
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

		items := []domain.ReserveItem{{ProductID: uuid.New(), Quantity: 2}}
		repo := &fakeRepository{releaseItems: items}
		svc := NewStockService(repo)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox_messages")).WillReturnError(errTestRepository)
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		if err := svc.HandleOrderEvent(context.Background(), tx, events.EventTypeOrderCancelled, uuid.New()); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})
}
