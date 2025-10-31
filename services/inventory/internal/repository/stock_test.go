package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

func TestStockRepository_GetByProductID(t *testing.T) {
	t.Run("returns the stock counters", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		productID := uuid.New()
		rows := sqlmock.NewRows([]string{"quantity_available", "quantity_reserved", "version"}).AddRow(10, 4, 2)
		mock.ExpectQuery("FROM inventory WHERE product_id").WithArgs(productID).WillReturnRows(rows)

		repo := NewStockRepository(db)
		got, err := repo.GetByProductID(context.Background(), productID)
		if err != nil {
			t.Fatalf("GetByProductID() error = %v", err)
		}
		if got.QuantityAvailable != 10 || got.QuantityReserved != 4 || got.Version != 2 {
			t.Errorf("GetByProductID() = %+v, want available=10 reserved=4 version=2", got)
		}
	})

	t.Run("returns not found when there is no row", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		productID := uuid.New()
		mock.ExpectQuery("FROM inventory WHERE product_id").WithArgs(productID).WillReturnError(sql.ErrNoRows)

		repo := NewStockRepository(db)
		_, err = repo.GetByProductID(context.Background(), productID)
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "PRODUCT_NOT_FOUND" {
			t.Errorf("error = %v, want PRODUCT_NOT_FOUND", err)
		}
	})

	t.Run("returns error on query failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		productID := uuid.New()
		mock.ExpectQuery("FROM inventory WHERE product_id").WithArgs(productID).WillReturnError(errors.New("boom"))

		repo := NewStockRepository(db)
		if _, err := repo.GetByProductID(context.Background(), productID); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestStockRepository_Reserve(t *testing.T) {
	t.Run("reserves every item in one transaction", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		item1 := domain.ReserveItem{ProductID: uuid.New(), Quantity: 2}
		item2 := domain.ReserveItem{ProductID: uuid.New(), Quantity: 3}

		mock.ExpectBegin()
		for _, item := range []domain.ReserveItem{item1, item2} {
			mock.ExpectQuery("SELECT 1 FROM inventory_reservations").
				WithArgs(orderID, item.ProductID, "reserved").
				WillReturnError(sql.ErrNoRows)
			mock.ExpectExec("UPDATE inventory").
				WithArgs(item.Quantity, item.ProductID).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO inventory_reservations").
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO inventory_movements").
				WithArgs(sqlmock.AnyArg(), item.ProductID, -item.Quantity, orderID).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}
		mock.ExpectCommit()

		repo := NewStockRepository(db)
		if err := repo.Reserve(context.Background(), orderID, []domain.ReserveItem{item1, item2}); err != nil {
			t.Fatalf("Reserve() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("rolls back the whole transaction on partial insufficiency", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		item1 := domain.ReserveItem{ProductID: uuid.New(), Quantity: 2}
		item2 := domain.ReserveItem{ProductID: uuid.New(), Quantity: 5}

		mock.ExpectBegin()

		// first item reserves cleanly
		mock.ExpectQuery("SELECT 1 FROM inventory_reservations").
			WithArgs(orderID, item1.ProductID, "reserved").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectExec("UPDATE inventory").
			WithArgs(item1.Quantity, item1.ProductID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO inventory_reservations").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO inventory_movements").
			WithArgs(sqlmock.AnyArg(), item1.ProductID, -item1.Quantity, orderID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		// second item is short on stock
		mock.ExpectQuery("SELECT 1 FROM inventory_reservations").
			WithArgs(orderID, item2.ProductID, "reserved").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectExec("UPDATE inventory").
			WithArgs(item2.Quantity, item2.ProductID).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT quantity_available FROM inventory").
			WithArgs(item2.ProductID).
			WillReturnRows(sqlmock.NewRows([]string{"quantity_available"}).AddRow(1))

		mock.ExpectRollback()

		repo := NewStockRepository(db)
		err = repo.Reserve(context.Background(), orderID, []domain.ReserveItem{item1, item2})
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "INSUFFICIENT_INVENTORY" {
			t.Errorf("error = %v, want INSUFFICIENT_INVENTORY", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("is idempotent for an item already reserved", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		item := domain.ReserveItem{ProductID: uuid.New(), Quantity: 2}

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT 1 FROM inventory_reservations").
			WithArgs(orderID, item.ProductID, "reserved").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
		mock.ExpectCommit()

		repo := NewStockRepository(db)
		if err := repo.Reserve(context.Background(), orderID, []domain.ReserveItem{item}); err != nil {
			t.Fatalf("Reserve() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error when the transaction fails to begin", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		mock.ExpectBegin().WillReturnError(errors.New("boom"))

		repo := NewStockRepository(db)
		if err := repo.Reserve(context.Background(), uuid.New(), []domain.ReserveItem{{ProductID: uuid.New(), Quantity: 1}}); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestStockRepository_Release(t *testing.T) {
	t.Run("releases every reserved item in one transaction", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()
		productID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery("FROM inventory_reservations").
			WithArgs(orderID, "reserved").
			WillReturnRows(sqlmock.NewRows([]string{"product_id", "quantity"}).AddRow(productID, 3))
		mock.ExpectExec("UPDATE inventory").
			WithArgs(3, productID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO inventory_movements").
			WithArgs(sqlmock.AnyArg(), productID, 3, orderID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("UPDATE inventory_reservations SET status").
			WithArgs("released", orderID, "reserved").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		repo := NewStockRepository(db)
		if err := repo.Release(context.Background(), orderID); err != nil {
			t.Fatalf("Release() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("is a no-op when there are no reservations to release", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery("FROM inventory_reservations").
			WithArgs(orderID, "reserved").
			WillReturnRows(sqlmock.NewRows([]string{"product_id", "quantity"}))
		mock.ExpectExec("UPDATE inventory_reservations SET status").
			WithArgs("released", orderID, "reserved").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		repo := NewStockRepository(db)
		if err := repo.Release(context.Background(), orderID); err != nil {
			t.Fatalf("Release() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error when the select fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		orderID := uuid.New()

		mock.ExpectBegin()
		mock.ExpectQuery("FROM inventory_reservations").
			WithArgs(orderID, "reserved").
			WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		repo := NewStockRepository(db)
		if err := repo.Release(context.Background(), orderID); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}
