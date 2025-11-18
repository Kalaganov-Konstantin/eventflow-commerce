// Package repository reads the payment_status read model.
package repository

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"time"

	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

const paymentStatusColumns = `id, customer_id, order_id, amount_cents, currency, status, transaction_id, created_at, updated_at, completed_at, version`

// PaymentStatus is a read model row projected from the payment event stream. Money fields are integer
// minor units (cents).
type PaymentStatus struct {
	ID            uuid.UUID
	CustomerID    uuid.UUID
	OrderID       uuid.UUID
	AmountCents   int64
	Currency      string
	Status        string
	TransactionID string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
	Version       int
}

// PaymentStatusRepository reads payment_status rows.
type PaymentStatusRepository struct {
	db *sql.DB
}

// NewPaymentStatusRepository builds a repository backed by the given database handle.
func NewPaymentStatusRepository(db *sql.DB) *PaymentStatusRepository {
	return &PaymentStatusRepository{db: db}
}

// GetByID returns the payment_status row for id.
func (r *PaymentStatusRepository) GetByID(ctx context.Context, id uuid.UUID) (*PaymentStatus, error) {
	status, err := r.scan(r.db.QueryRowContext(ctx, `
		SELECT `+paymentStatusColumns+` FROM payment_status WHERE id = $1
	`, id))
	if stderrors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.NewNotFound("payment")
	}
	if err != nil {
		return nil, fmt.Errorf("select payment status: %w", err)
	}
	return status, nil
}

// ListByOrderID returns every payment recorded for orderID, most recent first.
func (r *PaymentStatusRepository) ListByOrderID(ctx context.Context, orderID uuid.UUID) ([]*PaymentStatus, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+paymentStatusColumns+` FROM payment_status WHERE order_id = $1
		ORDER BY created_at DESC
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("select payment status by order id: %w", err)
	}
	defer func() { _ = rows.Close() }()

	statuses := make([]*PaymentStatus, 0)
	for rows.Next() {
		status, err := r.scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan payment status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment status: %w", err)
	}
	return statuses, nil
}

// rowScanner is satisfied by both sql.Row and sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func (r *PaymentStatusRepository) scan(row rowScanner) (*PaymentStatus, error) {
	var status PaymentStatus
	var transactionID sql.NullString
	var completedAt sql.NullTime

	if err := row.Scan(&status.ID, &status.CustomerID, &status.OrderID, &status.AmountCents, &status.Currency,
		&status.Status, &transactionID, &status.CreatedAt, &status.UpdatedAt, &completedAt, &status.Version); err != nil {
		return nil, err
	}
	status.TransactionID = transactionID.String
	if completedAt.Valid {
		completedAtValue := completedAt.Time
		status.CompletedAt = &completedAtValue
	}

	return &status, nil
}
