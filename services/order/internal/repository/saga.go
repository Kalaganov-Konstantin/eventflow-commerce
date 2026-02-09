package repository

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/saga"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// Saga is a snapshot of an order's saga row.
type Saga struct {
	OrderID       uuid.UUID
	State         saga.State
	ReservationID uuid.UUID
	PaymentID     uuid.UUID
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SagaRepository persists order saga state in postgres.
type SagaRepository struct {
	db *sql.DB
}

// NewSagaRepository builds a repository backed by the given database handle.
func NewSagaRepository(db *sql.DB) *SagaRepository {
	return &SagaRepository{db: db}
}

// Start inserts a new saga row for orderID at saga.StateStarted, within tx.
func (r *SagaRepository) Start(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO order_sagas (order_id, state) VALUES ($1, $2)
	`, orderID, string(saga.StateStarted)); err != nil {
		return fmt.Errorf("insert saga: %w", err)
	}
	return nil
}

// Transition moves the saga for orderID to `to`, validating the move against the saga's state
// graph. The current state is read with a row lock so concurrent transitions serialize. A
// transition to the state the saga is already in is a no-op. An order with no saga row, or a move
// the state graph rejects, returns an error and leaves the row untouched.
func (r *SagaRepository) Transition(ctx context.Context, tx *sql.Tx, orderID uuid.UUID, to saga.State) error {
	current, err := r.stateForUpdate(ctx, tx, orderID)
	if err != nil {
		return err
	}

	if err := saga.Transition(current, to); err != nil {
		return apperrors.NewConflict(err.Error())
	}
	if current == to {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE order_sagas SET state = $1 WHERE order_id = $2
	`, string(to), orderID); err != nil {
		return fmt.Errorf("update saga state: %w", err)
	}
	return nil
}

func (r *SagaRepository) stateForUpdate(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) (saga.State, error) {
	var state string
	err := tx.QueryRowContext(ctx, `
		SELECT state FROM order_sagas WHERE order_id = $1 FOR UPDATE
	`, orderID).Scan(&state)
	if stderrors.Is(err, sql.ErrNoRows) {
		return "", apperrors.NewNotFound("order saga")
	}
	if err != nil {
		return "", fmt.Errorf("select saga state: %w", err)
	}
	return saga.State(state), nil
}

// SetReservationID records the inventory reservation correlated with orderID's saga, within tx.
func (r *SagaRepository) SetReservationID(ctx context.Context, tx *sql.Tx, orderID, reservationID uuid.UUID) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE order_sagas SET reservation_id = $1 WHERE order_id = $2
	`, reservationID, orderID); err != nil {
		return fmt.Errorf("set saga reservation id: %w", err)
	}
	return nil
}

// SetPaymentID records the payment correlated with orderID's saga, within tx.
func (r *SagaRepository) SetPaymentID(ctx context.Context, tx *sql.Tx, orderID, paymentID uuid.UUID) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE order_sagas SET payment_id = $1 WHERE order_id = $2
	`, paymentID, orderID); err != nil {
		return fmt.Errorf("set saga payment id: %w", err)
	}
	return nil
}

// SetLastError records the most recent compensation failure for orderID's saga, so operators can
// see why a saga is stuck compensating. It uses its own connection rather than tx, since it is
// meant to survive the rollback of the transaction that hit the failure.
func (r *SagaRepository) SetLastError(ctx context.Context, orderID uuid.UUID, message string) error {
	if _, err := r.db.ExecContext(ctx, `
		UPDATE order_sagas SET last_error = $1 WHERE order_id = $2
	`, message, orderID); err != nil {
		return fmt.Errorf("set saga last error: %w", err)
	}
	return nil
}

// Get returns the current saga row for orderID.
func (r *SagaRepository) Get(ctx context.Context, orderID uuid.UUID) (*Saga, error) {
	var (
		s                        Saga
		state                    string
		reservationID, paymentID uuid.NullUUID
		lastError                sql.NullString
	)

	err := r.db.QueryRowContext(ctx, `
		SELECT order_id, state, reservation_id, payment_id, last_error, created_at, updated_at
		FROM order_sagas WHERE order_id = $1
	`, orderID).Scan(&s.OrderID, &state, &reservationID, &paymentID, &lastError, &s.CreatedAt, &s.UpdatedAt)
	if stderrors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.NewNotFound("order saga")
	}
	if err != nil {
		return nil, fmt.Errorf("select saga: %w", err)
	}

	s.State = saga.State(state)
	s.ReservationID = reservationID.UUID
	s.PaymentID = paymentID.UUID
	s.LastError = lastError.String

	return &s, nil
}
