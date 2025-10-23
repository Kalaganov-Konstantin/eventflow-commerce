package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

func newTestOrder() *domain.Order {
	now := time.Now().UTC()
	orderID := uuid.New()

	return &domain.Order{
		ID:               orderID,
		CustomerID:       uuid.New(),
		Status:           domain.StatusPending,
		TotalAmountCents: 1998,
		Currency:         "USD",
		CreatedAt:        now,
		UpdatedAt:        now,
		Version:          1,
		Items: []domain.OrderItem{
			{
				ID:              uuid.New(),
				ProductID:       uuid.New(),
				ProductName:     "Widget",
				ProductSKU:      "WID-1",
				Quantity:        2,
				UnitPriceCents:  999,
				TotalPriceCents: 1998,
			},
		},
	}
}

func TestOrderRepository_Save(t *testing.T) {
	t.Run("commits order and items", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder()

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO orders").
			WithArgs(order.ID, order.CustomerID, string(order.Status), order.TotalAmountCents, order.Currency,
				order.CreatedAt, order.UpdatedAt, order.Version).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO order_items").
			WithArgs(order.Items[0].ID, order.ID, order.Items[0].ProductID, order.Items[0].ProductName,
				order.Items[0].ProductSKU, order.Items[0].Quantity, order.Items[0].UnitPriceCents, order.Items[0].TotalPriceCents).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		repo := NewOrderRepository(db)
		if err := repo.Save(context.Background(), order); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("rolls back when order insert fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder()

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO orders").WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		repo := NewOrderRepository(db)
		if err := repo.Save(context.Background(), order); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("rolls back when item insert fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder()

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO orders").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO order_items").WillReturnError(errors.New("boom"))
		mock.ExpectRollback()

		repo := NewOrderRepository(db)
		if err := repo.Save(context.Background(), order); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error when commit fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		order := newTestOrder()

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO orders").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO order_items").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit().WillReturnError(errors.New("boom"))

		repo := NewOrderRepository(db)
		if err := repo.Save(context.Background(), order); err == nil {
			t.Fatal("expected error, got none")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})
}

func TestOrderRepository_GetByID(t *testing.T) {
	t.Run("assembles order and items", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		want := newTestOrder()

		orderRows := sqlmock.NewRows([]string{"id", "customer_id", "status", "total_amount_cents", "currency", "created_at", "updated_at", "version"}).
			AddRow(want.ID.String(), want.CustomerID.String(), string(want.Status), want.TotalAmountCents, want.Currency,
				want.CreatedAt, want.UpdatedAt, want.Version)
		mock.ExpectQuery("FROM orders WHERE id").WithArgs(want.ID).WillReturnRows(orderRows)

		itemRows := sqlmock.NewRows([]string{"id", "product_id", "product_name", "product_sku", "quantity", "unit_price_cents", "total_price_cents"}).
			AddRow(want.Items[0].ID.String(), want.Items[0].ProductID.String(), want.Items[0].ProductName,
				want.Items[0].ProductSKU, want.Items[0].Quantity, want.Items[0].UnitPriceCents, want.Items[0].TotalPriceCents)
		mock.ExpectQuery("FROM order_items WHERE order_id").WithArgs(want.ID).WillReturnRows(itemRows)

		repo := NewOrderRepository(db)
		got, err := repo.GetByID(context.Background(), want.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if got.ID != want.ID || got.CustomerID != want.CustomerID || got.Status != want.Status {
			t.Errorf("GetByID() = %+v, want %+v", got, want)
		}
		if len(got.Items) != 1 || got.Items[0].ID != want.Items[0].ID {
			t.Errorf("GetByID() items = %+v, want %+v", got.Items, want.Items)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns not found when there is no row", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		id := uuid.New()
		mock.ExpectQuery("FROM orders WHERE id").WithArgs(id).WillReturnError(sql.ErrNoRows)

		repo := NewOrderRepository(db)
		_, err = repo.GetByID(context.Background(), id)
		if err == nil {
			t.Fatal("expected error, got none")
		}
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "ORDER_NOT_FOUND" {
			t.Errorf("error = %v, want ORDER_NOT_FOUND", err)
		}
	})

	t.Run("returns error on query failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		id := uuid.New()
		mock.ExpectQuery("FROM orders WHERE id").WithArgs(id).WillReturnError(errors.New("boom"))

		repo := NewOrderRepository(db)
		if _, err := repo.GetByID(context.Background(), id); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestOrderRepository_ListByCustomer(t *testing.T) {
	t.Run("returns a page of orders", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		want := newTestOrder()
		rows := sqlmock.NewRows([]string{"id", "customer_id", "status", "total_amount_cents", "currency", "created_at", "updated_at", "version"}).
			AddRow(want.ID.String(), want.CustomerID.String(), string(want.Status), want.TotalAmountCents, want.Currency,
				want.CreatedAt, want.UpdatedAt, want.Version)
		mock.ExpectQuery("FROM orders WHERE customer_id").WithArgs(want.CustomerID, 20, 0).WillReturnRows(rows)

		repo := NewOrderRepository(db)
		got, err := repo.ListByCustomer(context.Background(), want.CustomerID, 20, 0)
		if err != nil {
			t.Fatalf("ListByCustomer() error = %v", err)
		}
		if len(got) != 1 || got[0].ID != want.ID {
			t.Errorf("ListByCustomer() = %+v, want one order with ID %v", got, want.ID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns an empty slice when there are no orders", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		customerID := uuid.New()
		rows := sqlmock.NewRows([]string{"id", "customer_id", "status", "total_amount_cents", "currency", "created_at", "updated_at", "version"})
		mock.ExpectQuery("FROM orders WHERE customer_id").WithArgs(customerID, 20, 0).WillReturnRows(rows)

		repo := NewOrderRepository(db)
		got, err := repo.ListByCustomer(context.Background(), customerID, 20, 0)
		if err != nil {
			t.Fatalf("ListByCustomer() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("ListByCustomer() = %+v, want empty", got)
		}
	})

	t.Run("returns error on query failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		customerID := uuid.New()
		mock.ExpectQuery("FROM orders WHERE customer_id").WithArgs(customerID, 20, 0).WillReturnError(errors.New("boom"))

		repo := NewOrderRepository(db)
		if _, err := repo.ListByCustomer(context.Background(), customerID, 20, 0); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestOrderRepository_UpdateStatus(t *testing.T) {
	t.Run("updates status and bumps version", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		id := uuid.New()
		mock.ExpectExec("UPDATE orders SET status").
			WithArgs(string(domain.StatusConfirmed), id, 1).
			WillReturnResult(sqlmock.NewResult(0, 1))

		repo := NewOrderRepository(db)
		if err := repo.UpdateStatus(context.Background(), id, domain.StatusConfirmed, 1); err != nil {
			t.Fatalf("UpdateStatus() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns conflict when the version does not match", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		id := uuid.New()
		mock.ExpectExec("UPDATE orders SET status").
			WithArgs(string(domain.StatusConfirmed), id, 1).
			WillReturnResult(sqlmock.NewResult(0, 0))

		repo := NewOrderRepository(db)
		err = repo.UpdateStatus(context.Background(), id, domain.StatusConfirmed, 1)
		if err == nil {
			t.Fatal("expected error, got none")
		}
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != "CONFLICT" {
			t.Errorf("error = %v, want CONFLICT", err)
		}
	})

	t.Run("returns error on exec failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		id := uuid.New()
		mock.ExpectExec("UPDATE orders SET status").
			WithArgs(string(domain.StatusConfirmed), id, 1).
			WillReturnError(errors.New("boom"))

		repo := NewOrderRepository(db)
		if err := repo.UpdateStatus(context.Background(), id, domain.StatusConfirmed, 1); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}
