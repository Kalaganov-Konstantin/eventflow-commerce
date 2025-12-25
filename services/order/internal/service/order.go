// Package service applies order lifecycle transitions on top of the repository.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/outbox"
	"github.com/google/uuid"
)

// Repository is the persistence port the order service depends on.
type Repository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Order, error)
	UpdateStatus(ctx context.Context, tx *sql.Tx, id uuid.UUID, status domain.Status, expectedVersion int) error
}

// OrderService applies order lifecycle transitions and records the resulting domain event in the
// outbox, within a transaction supplied by the caller so it can be combined with other writes.
type OrderService struct {
	repo   Repository
	outbox *outbox.Store
}

// NewOrderService builds an OrderService backed by repo.
func NewOrderService(repo Repository) *OrderService {
	return &OrderService{repo: repo, outbox: outbox.NewStore()}
}

// MarkPendingPayment transitions order to pending_payment and enqueues order.ready_for_payment.
func (s *OrderService) MarkPendingPayment(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	return s.applyTransition(ctx, tx, order, (*domain.Order).MarkPendingPayment, events.EventTypeOrderReadyForPayment)
}

// ConfirmPayment transitions order to confirmed and enqueues order.confirmed. An order that
// already reached a terminal status is left untouched, since a redelivered or late payment event
// is not an error.
func (s *OrderService) ConfirmPayment(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if isTerminal(order.Status) {
		return nil
	}
	return s.applyTransition(ctx, tx, order, (*domain.Order).Confirm, events.EventTypeOrderConfirmed)
}

// FailPayment transitions order to payment_failed and enqueues order.cancelled: the event catalog
// has no dedicated payment-failed type, and downstream consumers treat the order the same way as
// an explicit cancellation. An order that already reached a terminal status is left untouched,
// since a redelivered or late payment event is not an error.
func (s *OrderService) FailPayment(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if isTerminal(order.Status) {
		return nil
	}
	return s.applyTransition(ctx, tx, order, (*domain.Order).Fail, events.EventTypeOrderCancelled)
}

// isTerminal reports whether status is an outcome a payment result would otherwise drive the
// order to, so a redelivered or late event can be recognized and ignored.
func isTerminal(status domain.Status) bool {
	switch status {
	case domain.StatusConfirmed, domain.StatusPaymentFailed, domain.StatusCancelled:
		return true
	default:
		return false
	}
}

// applyTransition runs apply on order, persists the resulting status and enqueues eventType, all
// within tx.
func (s *OrderService) applyTransition(ctx context.Context, tx *sql.Tx, order *domain.Order, apply func(*domain.Order) error, eventType string) error {
	expectedVersion := order.Version

	if err := apply(order); err != nil {
		return err
	}

	if err := s.repo.UpdateStatus(ctx, tx, order.ID, order.Status, expectedVersion); err != nil {
		return err
	}

	payload, err := json.Marshal(newOrderStatusPayload(order))
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", eventType, err)
	}

	return s.outbox.Enqueue(ctx, tx, outbox.Message{
		Topic:       events.OrdersTopic,
		EventType:   eventType,
		AggregateID: order.ID.String(),
		Payload:     payload,
	})
}

// orderStatusPayload is the outbox payload for an order status transition. Money fields are
// integer minor units (cents).
type orderStatusPayload struct {
	OrderID          string `json:"order_id"`
	CustomerID       string `json:"customer_id"`
	Status           string `json:"status"`
	TotalAmountCents int64  `json:"total_amount_cents"`
	Currency         string `json:"currency"`
}

func newOrderStatusPayload(o *domain.Order) orderStatusPayload {
	return orderStatusPayload{
		OrderID:          o.ID.String(),
		CustomerID:       o.CustomerID.String(),
		Status:           string(o.Status),
		TotalAmountCents: o.TotalAmountCents,
		Currency:         o.Currency,
	}
}
