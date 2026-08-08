package repository

import (
	"context"
	"database/sql"
	stderrors "errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/saga"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

func TestSagaRepository_Start(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO order_sagas")).
		WithArgs(orderID, string(saga.StateStarted)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	repo := NewSagaRepository(db)
	if err := repo.Start(context.Background(), tx, orderID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSagaRepository_Start_ExecFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO order_sagas")).
		WithArgs(orderID, string(saga.StateStarted)).
		WillReturnError(stderrors.New("boom"))
	mock.ExpectRollback()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	repo := NewSagaRepository(db)
	if err := repo.Start(context.Background(), tx, orderID); err == nil {
		t.Fatal("expected error, got none")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSagaRepository_Transition(t *testing.T) {
	t.Run("valid transition updates the state", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT state FROM order_sagas WHERE order_id = $1 FOR UPDATE")).
			WithArgs(orderID).
			WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(string(saga.StateStarted)))
		mock.ExpectExec(regexp.QuoteMeta("UPDATE order_sagas SET state = $1 WHERE order_id = $2")).
			WithArgs(string(saga.StateStockReserved), orderID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		repo := NewSagaRepository(db)
		if err := repo.Transition(context.Background(), tx, orderID, saga.StateStockReserved); err != nil {
			t.Fatalf("Transition() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("tx.Commit() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("retrying the current state is a no-op", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT state FROM order_sagas WHERE order_id = $1 FOR UPDATE")).
			WithArgs(orderID).
			WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(string(saga.StateCompensating)))
		mock.ExpectCommit()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		repo := NewSagaRepository(db)
		if err := repo.Transition(context.Background(), tx, orderID, saga.StateCompensating); err != nil {
			t.Fatalf("Transition() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("tx.Commit() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("rejects an invalid transition without writing", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT state FROM order_sagas WHERE order_id = $1 FOR UPDATE")).
			WithArgs(orderID).
			WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(string(saga.StateCompleted)))
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		repo := NewSagaRepository(db)
		err = repo.Transition(context.Background(), tx, orderID, saga.StateStockReserved)
		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) || appErr.Code != "CONFLICT" {
			t.Errorf("error = %v, want CONFLICT", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error when reading the current state fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT state FROM order_sagas WHERE order_id = $1 FOR UPDATE")).
			WithArgs(orderID).
			WillReturnError(stderrors.New("boom"))
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		repo := NewSagaRepository(db)
		if err := repo.Transition(context.Background(), tx, orderID, saga.StateStockReserved); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error when the update fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT state FROM order_sagas WHERE order_id = $1 FOR UPDATE")).
			WithArgs(orderID).
			WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(string(saga.StateStarted)))
		mock.ExpectExec(regexp.QuoteMeta("UPDATE order_sagas SET state = $1 WHERE order_id = $2")).
			WithArgs(string(saga.StateStockReserved), orderID).
			WillReturnError(stderrors.New("boom"))
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		repo := NewSagaRepository(db)
		if err := repo.Transition(context.Background(), tx, orderID, saga.StateStockReserved); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("an order with no saga row is not found", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT state FROM order_sagas WHERE order_id = $1 FOR UPDATE")).
			WithArgs(orderID).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("db.Begin() error = %v", err)
		}

		repo := NewSagaRepository(db)
		err = repo.Transition(context.Background(), tx, orderID, saga.StateStockReserved)
		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) || appErr.Code != "NOT_FOUND" {
			t.Errorf("error = %v, want NOT_FOUND", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("tx.Rollback() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})
}

func TestSagaRepository_SetReservationID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID := uuid.New()
	reservationID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE order_sagas SET reservation_id = $1 WHERE order_id = $2")).
		WithArgs(reservationID, orderID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	repo := NewSagaRepository(db)
	if err := repo.SetReservationID(context.Background(), tx, orderID, reservationID); err != nil {
		t.Fatalf("SetReservationID() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSagaRepository_SetReservationID_ExecFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID := uuid.New()
	reservationID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE order_sagas SET reservation_id = $1 WHERE order_id = $2")).
		WithArgs(reservationID, orderID).
		WillReturnError(stderrors.New("boom"))
	mock.ExpectRollback()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	repo := NewSagaRepository(db)
	if err := repo.SetReservationID(context.Background(), tx, orderID, reservationID); err == nil {
		t.Fatal("expected error, got none")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSagaRepository_SetPaymentID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID := uuid.New()
	paymentID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE order_sagas SET payment_id = $1 WHERE order_id = $2")).
		WithArgs(paymentID, orderID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	repo := NewSagaRepository(db)
	if err := repo.SetPaymentID(context.Background(), tx, orderID, paymentID); err != nil {
		t.Fatalf("SetPaymentID() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSagaRepository_SetPaymentID_ExecFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID := uuid.New()
	paymentID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE order_sagas SET payment_id = $1 WHERE order_id = $2")).
		WithArgs(paymentID, orderID).
		WillReturnError(stderrors.New("boom"))
	mock.ExpectRollback()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() error = %v", err)
	}

	repo := NewSagaRepository(db)
	if err := repo.SetPaymentID(context.Background(), tx, orderID, paymentID); err == nil {
		t.Fatal("expected error, got none")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSagaRepository_SetLastError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE order_sagas SET last_error = $1 WHERE order_id = $2")).
		WithArgs("release reservation: timed out", orderID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewSagaRepository(db)
	if err := repo.SetLastError(context.Background(), orderID, "release reservation: timed out"); err != nil {
		t.Fatalf("SetLastError() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSagaRepository_SetLastError_ExecFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	orderID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE order_sagas SET last_error = $1 WHERE order_id = $2")).
		WithArgs("release reservation: timed out", orderID).
		WillReturnError(stderrors.New("boom"))

	repo := NewSagaRepository(db)
	if err := repo.SetLastError(context.Background(), orderID, "release reservation: timed out"); err == nil {
		t.Fatal("expected error, got none")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSagaRepository_Get(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		paymentID := uuid.New()
		now := time.Now().UTC()

		rows := sqlmock.NewRows([]string{"order_id", "state", "reservation_id", "payment_id", "last_error", "created_at", "updated_at"}).
			AddRow(orderID, string(saga.StateAwaitingPayment), nil, paymentID, nil, now, now)
		mock.ExpectQuery(regexp.QuoteMeta("SELECT order_id, state, reservation_id, payment_id, last_error, created_at, updated_at")).
			WithArgs(orderID).
			WillReturnRows(rows)

		repo := NewSagaRepository(db)
		got, err := repo.Get(context.Background(), orderID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.State != saga.StateAwaitingPayment {
			t.Errorf("State = %v, want %v", got.State, saga.StateAwaitingPayment)
		}
		if got.PaymentID != paymentID {
			t.Errorf("PaymentID = %v, want %v", got.PaymentID, paymentID)
		}
		if got.ReservationID != uuid.Nil {
			t.Errorf("ReservationID = %v, want nil", got.ReservationID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT order_id, state, reservation_id, payment_id, last_error, created_at, updated_at")).
			WithArgs(orderID).
			WillReturnError(sql.ErrNoRows)

		repo := NewSagaRepository(db)
		_, err = repo.Get(context.Background(), orderID)
		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) || appErr.Code != "NOT_FOUND" {
			t.Errorf("error = %v, want NOT_FOUND", err)
		}
	})

	t.Run("returns error on query failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT order_id, state, reservation_id, payment_id, last_error, created_at, updated_at")).
			WithArgs(orderID).
			WillReturnError(stderrors.New("boom"))

		repo := NewSagaRepository(db)
		if _, err := repo.Get(context.Background(), orderID); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}
