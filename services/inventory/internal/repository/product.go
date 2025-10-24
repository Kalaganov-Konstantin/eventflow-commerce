// Package repository persists the inventory aggregates in postgres.
package repository

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

const productColumns = `id, name, description, sku, category, brand, price_cents, cost_cents, currency, is_active, created_at, updated_at, version`

// ProductRepository stores and retrieves products in postgres.
type ProductRepository struct {
	db *sql.DB
}

// NewProductRepository builds a repository backed by the given database handle.
func NewProductRepository(db *sql.DB) *ProductRepository {
	return &ProductRepository{db: db}
}

// GetByID returns the product with the given id.
func (r *ProductRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Product, error) {
	product, err := r.scanProduct(r.db.QueryRowContext(ctx, `
		SELECT `+productColumns+` FROM products WHERE id = $1
	`, id))
	if stderrors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.NewProductNotFound(id.String())
	}
	if err != nil {
		return nil, fmt.Errorf("select product: %w", err)
	}
	return product, nil
}

// GetBySKU returns the product with the given SKU.
func (r *ProductRepository) GetBySKU(ctx context.Context, sku string) (*domain.Product, error) {
	product, err := r.scanProduct(r.db.QueryRowContext(ctx, `
		SELECT `+productColumns+` FROM products WHERE sku = $1
	`, sku))
	if stderrors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.NewProductNotFound(sku)
	}
	if err != nil {
		return nil, fmt.Errorf("select product: %w", err)
	}
	return product, nil
}

// List returns a page of products, optionally narrowed by category and active status.
func (r *ProductRepository) List(ctx context.Context, category string, activeOnly bool, limit, offset int) ([]*domain.Product, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+productColumns+` FROM products
		WHERE ($1 = '' OR category = $1) AND (NOT $2 OR is_active = true)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, category, activeOnly, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("select products: %w", err)
	}
	defer func() { _ = rows.Close() }()

	products := make([]*domain.Product, 0)
	for rows.Next() {
		product, err := r.scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate products: %w", err)
	}

	return products, nil
}

// Create inserts a new product.
func (r *ProductRepository) Create(ctx context.Context, product *domain.Product) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO products (`+productColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, product.ID, product.Name, product.Description, product.SKU, product.Category, product.Brand,
		product.PriceCents, product.CostCents, product.Currency, product.IsActive,
		product.CreatedAt, product.UpdatedAt, product.Version)
	if err != nil {
		return fmt.Errorf("insert product: %w", err)
	}
	return nil
}

// Update writes product's mutable fields, using product.Version as an optimistic lock.
func (r *ProductRepository) Update(ctx context.Context, product *domain.Product) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE products
		SET name = $1, description = $2, category = $3, brand = $4,
		    price_cents = $5, cost_cents = $6, currency = $7, is_active = $8, version = version + 1
		WHERE id = $9 AND version = $10
	`, product.Name, product.Description, product.Category, product.Brand,
		product.PriceCents, product.CostCents, product.Currency, product.IsActive,
		product.ID, product.Version)
	if err != nil {
		return fmt.Errorf("update product: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if rows == 0 {
		return apperrors.NewConflict(fmt.Sprintf("product %s was modified concurrently", product.ID))
	}
	return nil
}

// rowScanner is satisfied by both sql.Row and sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func (r *ProductRepository) scanProduct(row rowScanner) (*domain.Product, error) {
	var product domain.Product
	var description, category, brand sql.NullString
	var costCents sql.NullInt64

	if err := row.Scan(&product.ID, &product.Name, &description, &product.SKU, &category, &brand,
		&product.PriceCents, &costCents, &product.Currency, &product.IsActive,
		&product.CreatedAt, &product.UpdatedAt, &product.Version); err != nil {
		return nil, err
	}
	product.Description = description.String
	product.Category = category.String
	product.Brand = brand.String
	product.CostCents = costCents.Int64

	return &product, nil
}
