// Package projection keeps the payment_status read model in sync with the payment event stream.
package projection

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
)

// PaymentStatus upserts payment_status rows from payment domain events. It is meant to run inside the
// same transaction as the event store write, so the read model never lags behind the event stream.
type PaymentStatus struct{}

// NewPaymentStatus builds a payment_status projector.
func NewPaymentStatus() *PaymentStatus {
	return &PaymentStatus{}
}

// Apply projects event onto the payment_status row for payment, inside tx. completed_at is only set
// when event is a PaymentProcessed; every other event only advances status and version.
func (p *PaymentStatus) Apply(ctx context.Context, tx *sql.Tx, payment *domain.Payment, event domain.Event) error {
	var transactionID sql.NullString
	var completedAt sql.NullTime
	if processed, ok := event.(*domain.PaymentProcessed); ok {
		transactionID = sql.NullString{String: processed.TransactionID, Valid: true}
		completedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO payment_status (id, customer_id, order_id, amount_cents, currency, status, transaction_id, created_at, updated_at, completed_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW(), $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			transaction_id = COALESCE(EXCLUDED.transaction_id, payment_status.transaction_id),
			updated_at = NOW(),
			completed_at = COALESCE(EXCLUDED.completed_at, payment_status.completed_at),
			version = EXCLUDED.version
	`, payment.ID, payment.CustomerID, payment.OrderID, payment.AmountCents, payment.Currency,
		string(payment.Status), transactionID, completedAt, payment.Version)
	if err != nil {
		return fmt.Errorf("upsert payment status: %w", err)
	}
	return nil
}
