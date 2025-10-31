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

// StockRepository manages inventory counters and reservations in postgres.
type StockRepository struct {
	db *sql.DB
}

// NewStockRepository builds a repository backed by the given database handle.
func NewStockRepository(db *sql.DB) *StockRepository {
	return &StockRepository{db: db}
}

// GetByProductID returns the current stock counters for a product.
func (r *StockRepository) GetByProductID(ctx context.Context, productID uuid.UUID) (*domain.Stock, error) {
	stock := &domain.Stock{ProductID: productID}
	err := r.db.QueryRowContext(ctx, `
		SELECT quantity_available, quantity_reserved, version
		FROM inventory WHERE product_id = $1
	`, productID).Scan(&stock.QuantityAvailable, &stock.QuantityReserved, &stock.Version)
	if stderrors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.NewProductNotFound(productID.String())
	}
	if err != nil {
		return nil, fmt.Errorf("select inventory: %w", err)
	}
	return stock, nil
}

// Reserve moves quantity from available to reserved for every item, in one transaction. A product
// with insufficient available quantity rolls back the whole reservation. A repeated call for an
// order/product pair that is already reserved is a no-op for that item.
func (r *StockRepository) Reserve(ctx context.Context, orderID uuid.UUID, items []domain.ReserveItem) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, item := range items {
		duplicate, err := r.hasReservedRow(ctx, tx, orderID, item.ProductID)
		if err != nil {
			return err
		}
		if duplicate {
			continue
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE inventory
			SET quantity_available = quantity_available - $1, quantity_reserved = quantity_reserved + $1
			WHERE product_id = $2 AND quantity_available >= $1
		`, item.Quantity, item.ProductID)
		if err != nil {
			return fmt.Errorf("update inventory: %w", err)
		}

		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read rows affected: %w", err)
		}
		if rows == 0 {
			available, err := r.availableQuantity(ctx, tx, item.ProductID)
			if err != nil {
				return err
			}
			return apperrors.NewInsufficientInventory(item.ProductID.String(), item.Quantity, available)
		}

		reservation, err := domain.NewReservation(orderID, item.ProductID, item.Quantity)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inventory_reservations (id, order_id, product_id, quantity, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, reservation.ID, reservation.OrderID, reservation.ProductID, reservation.Quantity,
			string(reservation.Status), reservation.CreatedAt, reservation.UpdatedAt); err != nil {
			return fmt.Errorf("insert reservation: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inventory_movements (id, product_id, movement_type, quantity, reference_id, reference_type)
			VALUES ($1, $2, 'reservation', $3, $4, 'order')
		`, uuid.New(), item.ProductID, -item.Quantity, orderID); err != nil {
			return fmt.Errorf("insert movement: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// Release returns every reserved item of an order to available stock, in one transaction. An order
// with no active reservations is a no-op.
func (r *StockRepository) Release(ctx context.Context, orderID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT product_id, quantity FROM inventory_reservations
		WHERE order_id = $1 AND status = $2
	`, orderID, string(domain.ReservationStatusReserved))
	if err != nil {
		return fmt.Errorf("select reservations: %w", err)
	}

	type reservedItem struct {
		productID uuid.UUID
		quantity  int
	}
	var reserved []reservedItem
	for rows.Next() {
		var item reservedItem
		if err := rows.Scan(&item.productID, &item.quantity); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan reservation: %w", err)
		}
		reserved = append(reserved, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate reservations: %w", err)
	}
	_ = rows.Close()

	for _, item := range reserved {
		if _, err := tx.ExecContext(ctx, `
			UPDATE inventory
			SET quantity_available = quantity_available + $1, quantity_reserved = quantity_reserved - $1
			WHERE product_id = $2
		`, item.quantity, item.productID); err != nil {
			return fmt.Errorf("update inventory: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inventory_movements (id, product_id, movement_type, quantity, reference_id, reference_type)
			VALUES ($1, $2, 'release', $3, $4, 'order')
		`, uuid.New(), item.productID, item.quantity, orderID); err != nil {
			return fmt.Errorf("insert movement: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE inventory_reservations SET status = $1 WHERE order_id = $2 AND status = $3
	`, string(domain.ReservationStatusReleased), orderID, string(domain.ReservationStatusReserved)); err != nil {
		return fmt.Errorf("release reservations: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (r *StockRepository) hasReservedRow(ctx context.Context, tx *sql.Tx, orderID, productID uuid.UUID) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1 FROM inventory_reservations WHERE order_id = $1 AND product_id = $2 AND status = $3
	`, orderID, productID, string(domain.ReservationStatusReserved)).Scan(&exists)
	if stderrors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("select existing reservation: %w", err)
	}
	return true, nil
}

func (r *StockRepository) availableQuantity(ctx context.Context, tx *sql.Tx, productID uuid.UUID) (int, error) {
	var available int
	err := tx.QueryRowContext(ctx, `SELECT quantity_available FROM inventory WHERE product_id = $1`, productID).Scan(&available)
	if stderrors.Is(err, sql.ErrNoRows) {
		return 0, apperrors.NewProductNotFound(productID.String())
	}
	if err != nil {
		return 0, fmt.Errorf("select available quantity: %w", err)
	}
	return available, nil
}
