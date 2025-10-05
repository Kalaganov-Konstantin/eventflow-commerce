// Package repository persists the order aggregate in postgres.
package repository

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// OrderRepository stores and retrieves orders in postgres.
type OrderRepository struct {
	db *sql.DB
}

// NewOrderRepository builds a repository backed by the given database handle.
func NewOrderRepository(db *sql.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// Save writes the order and its items in a single transaction.
func (r *OrderRepository) Save(ctx context.Context, order *domain.Order) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO orders (id, customer_id, status, total_amount, currency, created_at, updated_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, order.ID, order.CustomerID, string(order.Status), order.TotalAmount, order.Currency,
		order.CreatedAt, order.UpdatedAt, order.Version)
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}

	for _, item := range order.Items {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO order_items (id, order_id, product_id, product_name, product_sku, quantity, unit_price, total_price)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, item.ID, order.ID, item.ProductID, item.ProductName, item.ProductSKU,
			item.Quantity, item.UnitPrice, item.TotalPrice)
		if err != nil {
			return fmt.Errorf("insert order item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// GetByID assembles the full order aggregate, including its items.
func (r *OrderRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Order, error) {
	order, err := r.scanOrder(r.db.QueryRowContext(ctx, `
		SELECT id, customer_id, status, total_amount, currency, created_at, updated_at, version
		FROM orders WHERE id = $1
	`, id))
	if stderrors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.NewOrderNotFound(id.String())
	}
	if err != nil {
		return nil, fmt.Errorf("select order: %w", err)
	}

	items, err := r.itemsByOrderID(ctx, id)
	if err != nil {
		return nil, err
	}
	order.Items = items

	return order, nil
}

// ListByCustomer returns a page of order summaries for the given customer, most recent first.
func (r *OrderRepository) ListByCustomer(ctx context.Context, customerID uuid.UUID, limit, offset int) ([]*domain.Order, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, customer_id, status, total_amount, currency, created_at, updated_at, version
		FROM orders WHERE customer_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, customerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("select orders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	orders := make([]*domain.Order, 0)
	for rows.Next() {
		order, err := r.scanOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orders: %w", err)
	}

	return orders, nil
}

// UpdateStatus transitions an order's status, using version as an optimistic lock.
func (r *OrderRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.Status, expectedVersion int) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE orders SET status = $1, version = version + 1
		WHERE id = $2 AND version = $3
	`, string(status), id, expectedVersion)
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if rows == 0 {
		return apperrors.NewConflict(fmt.Sprintf("order %s was modified concurrently", id))
	}
	return nil
}

// rowScanner is satisfied by both sql.Row and sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func (r *OrderRepository) scanOrder(row rowScanner) (*domain.Order, error) {
	var order domain.Order
	var status string

	if err := row.Scan(&order.ID, &order.CustomerID, &status, &order.TotalAmount, &order.Currency,
		&order.CreatedAt, &order.UpdatedAt, &order.Version); err != nil {
		return nil, err
	}
	order.Status = domain.Status(status)

	return &order, nil
}

func (r *OrderRepository) itemsByOrderID(ctx context.Context, orderID uuid.UUID) ([]domain.OrderItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, product_id, product_name, product_sku, quantity, unit_price, total_price
		FROM order_items WHERE order_id = $1
		ORDER BY created_at
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("select order items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domain.OrderItem, 0)
	for rows.Next() {
		var item domain.OrderItem
		var sku sql.NullString
		if err := rows.Scan(&item.ID, &item.ProductID, &item.ProductName, &sku,
			&item.Quantity, &item.UnitPrice, &item.TotalPrice); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		item.ProductSKU = sku.String
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order items: %w", err)
	}

	return items, nil
}
