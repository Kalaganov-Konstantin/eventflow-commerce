package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

var productColumnNames = []string{
	"id", "name", "description", "sku", "category", "brand",
	"price_cents", "cost_cents", "currency", "is_active", "created_at", "updated_at", "version",
}

func newTestProduct() *domain.Product {
	now := time.Now().UTC()
	return &domain.Product{
		ID:          uuid.New(),
		Name:        "Widget",
		Description: "A widget",
		SKU:         "WID-1",
		Category:    "gadgets",
		Brand:       "Acme",
		PriceCents:  999,
		CostCents:   500,
		Currency:    "USD",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     1,
	}
}

func productRow(p *domain.Product) *sqlmock.Rows {
	return sqlmock.NewRows(productColumnNames).AddRow(
		p.ID.String(), p.Name, p.Description, p.SKU, p.Category, p.Brand,
		p.PriceCents, p.CostCents, p.Currency, p.IsActive, p.CreatedAt, p.UpdatedAt, p.Version,
	)
}

func TestProductRepository_GetByID(t *testing.T) {
	t.Run("returns the product", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		want := newTestProduct()
		mock.ExpectQuery("FROM products WHERE id").WithArgs(want.ID).WillReturnRows(productRow(want))

		repo := NewProductRepository(db)
		got, err := repo.GetByID(context.Background(), want.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if got.ID != want.ID || got.SKU != want.SKU || got.PriceCents != want.PriceCents {
			t.Errorf("GetByID() = %+v, want %+v", got, want)
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
		mock.ExpectQuery("FROM products WHERE id").WithArgs(id).WillReturnError(sql.ErrNoRows)

		repo := NewProductRepository(db)
		_, err = repo.GetByID(context.Background(), id)
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

		id := uuid.New()
		mock.ExpectQuery("FROM products WHERE id").WithArgs(id).WillReturnError(errors.New("boom"))

		repo := NewProductRepository(db)
		if _, err := repo.GetByID(context.Background(), id); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestProductRepository_GetBySKU(t *testing.T) {
	t.Run("returns the product", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		want := newTestProduct()
		mock.ExpectQuery("FROM products WHERE sku").WithArgs(want.SKU).WillReturnRows(productRow(want))

		repo := NewProductRepository(db)
		got, err := repo.GetBySKU(context.Background(), want.SKU)
		if err != nil {
			t.Fatalf("GetBySKU() error = %v", err)
		}
		if got.SKU != want.SKU {
			t.Errorf("GetBySKU() = %+v, want %+v", got, want)
		}
	})

	t.Run("returns not found when there is no row", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		mock.ExpectQuery("FROM products WHERE sku").WithArgs("MISSING").WillReturnError(sql.ErrNoRows)

		repo := NewProductRepository(db)
		_, err = repo.GetBySKU(context.Background(), "MISSING")
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

		mock.ExpectQuery("FROM products WHERE sku").WithArgs("WID-1").WillReturnError(errors.New("boom"))

		repo := NewProductRepository(db)
		if _, err := repo.GetBySKU(context.Background(), "WID-1"); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestProductRepository_List(t *testing.T) {
	t.Run("returns a page of products", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		want := newTestProduct()
		mock.ExpectQuery("FROM products WHERE").
			WithArgs("gadgets", true, 20, 0).
			WillReturnRows(productRow(want))

		repo := NewProductRepository(db)
		got, err := repo.List(context.Background(), "gadgets", true, 20, 0)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(got) != 1 || got[0].ID != want.ID {
			t.Errorf("List() = %+v, want one product with ID %v", got, want.ID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns an empty slice when there are no products", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		mock.ExpectQuery("FROM products WHERE").
			WithArgs("", false, 20, 0).
			WillReturnRows(sqlmock.NewRows(productColumnNames))

		repo := NewProductRepository(db)
		got, err := repo.List(context.Background(), "", false, 20, 0)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("List() = %+v, want empty", got)
		}
	})

	t.Run("returns error on query failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		mock.ExpectQuery("FROM products WHERE").
			WithArgs("", false, 20, 0).
			WillReturnError(errors.New("boom"))

		repo := NewProductRepository(db)
		if _, err := repo.List(context.Background(), "", false, 20, 0); err == nil {
			t.Fatal("expected error, got none")
		}
	})

	t.Run("returns error when a row fails to scan", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		rows := sqlmock.NewRows([]string{"id"}).AddRow("only-one-column")
		mock.ExpectQuery("FROM products WHERE").
			WithArgs("", false, 20, 0).
			WillReturnRows(rows)

		repo := NewProductRepository(db)
		if _, err := repo.List(context.Background(), "", false, 20, 0); err == nil {
			t.Fatal("expected error, got none")
		}
	})

	t.Run("returns error when row iteration fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		rows := productRow(newTestProduct()).RowError(0, errors.New("boom"))
		mock.ExpectQuery("FROM products WHERE").
			WithArgs("", false, 20, 0).
			WillReturnRows(rows)

		repo := NewProductRepository(db)
		if _, err := repo.List(context.Background(), "", false, 20, 0); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestProductRepository_Create(t *testing.T) {
	t.Run("inserts the product", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		product := newTestProduct()
		mock.ExpectExec("INSERT INTO products").WillReturnResult(sqlmock.NewResult(0, 1))

		repo := NewProductRepository(db)
		if err := repo.Create(context.Background(), product); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("returns error on insert failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		mock.ExpectExec("INSERT INTO products").WillReturnError(errors.New("boom"))

		repo := NewProductRepository(db)
		if err := repo.Create(context.Background(), newTestProduct()); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}

func TestProductRepository_Update(t *testing.T) {
	t.Run("updates the product and bumps version", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		product := newTestProduct()
		mock.ExpectExec("UPDATE products SET").WillReturnResult(sqlmock.NewResult(0, 1))

		repo := NewProductRepository(db)
		if err := repo.Update(context.Background(), product); err != nil {
			t.Fatalf("Update() error = %v", err)
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

		mock.ExpectExec("UPDATE products SET").WillReturnResult(sqlmock.NewResult(0, 0))

		repo := NewProductRepository(db)
		err = repo.Update(context.Background(), newTestProduct())
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

		mock.ExpectExec("UPDATE products SET").WillReturnError(errors.New("boom"))

		repo := NewProductRepository(db)
		if err := repo.Update(context.Background(), newTestProduct()); err == nil {
			t.Fatal("expected error, got none")
		}
	})

	t.Run("returns error when rows affected read fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() { _ = db.Close() }()

		mock.ExpectExec("UPDATE products SET").WillReturnResult(sqlmock.NewErrorResult(errors.New("boom")))

		repo := NewProductRepository(db)
		if err := repo.Update(context.Background(), newTestProduct()); err == nil {
			t.Fatal("expected error, got none")
		}
	})
}
