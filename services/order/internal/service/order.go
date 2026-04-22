// Package service applies order lifecycle transitions on top of the repository.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/saga"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/outbox"
	"github.com/google/uuid"
)

// orderCacheTTL is how long a cached order read stays valid.
const orderCacheTTL = 60 * time.Second

// Repository is the persistence port the order service depends on.
type Repository interface {
	Save(ctx context.Context, order *domain.Order) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Order, error)
	ListByCustomer(ctx context.Context, customerID uuid.UUID, limit, offset int) ([]*domain.Order, error)
	UpdateStatus(ctx context.Context, tx *sql.Tx, id uuid.UUID, status domain.Status, expectedVersion int) error
}

// OrderCache is the cache-aside port GetByID reads and fills for read-only order lookups. A nil
// OrderCache disables caching, which is how the service degrades when Redis is unavailable at
// startup. Lifecycle transitions read the repository directly instead, so they never act on a
// stale cached version.
type OrderCache interface {
	GetJSON(ctx context.Context, key string, dest any) (bool, error)
	SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error
}

// SagaRepository is the persistence port for the order saga state that accompanies each order
// through the steps above and, if one of them fails, its compensation.
type SagaRepository interface {
	Start(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error
	Transition(ctx context.Context, tx *sql.Tx, orderID uuid.UUID, to saga.State) error
	SetLastError(ctx context.Context, orderID uuid.UUID, message string) error
}

// InventoryReleaser is the port the order service uses to give back a stock reservation while
// compensating a failed saga.
type InventoryReleaser interface {
	Release(ctx context.Context, orderID uuid.UUID) error
}

// PaymentRefunder is the port the order service uses to refund a payment while compensating a
// saga that fails after the payment already succeeded.
type PaymentRefunder interface {
	Refund(ctx context.Context, paymentID uuid.UUID, reason string) error
}

// OrderService applies order lifecycle transitions and records the resulting domain event in the
// outbox, within a transaction supplied by the caller so it can be combined with other writes.
type OrderService struct {
	repo      Repository
	db        *sql.DB
	saga      SagaRepository
	inventory InventoryReleaser
	payments  PaymentRefunder
	outbox    *outbox.Store
	cache     OrderCache
}

// NewOrderService builds an OrderService backed by repo, sagaRepo, inventory and payments. db is
// used only for transitions that have no caller-supplied transaction to join, such as
// MarkPendingPaymentAfterCreate and the saga's durable compensating checkpoint. The service
// starts without a cache; call SetCache to enable one.
func NewOrderService(repo Repository, db *sql.DB, sagaRepo SagaRepository, inventory InventoryReleaser, payments PaymentRefunder) *OrderService {
	return &OrderService{repo: repo, db: db, saga: sagaRepo, inventory: inventory, payments: payments, outbox: outbox.NewStore()}
}

// SetCache attaches cache for GetByID reads. Passing nil disables caching.
func (s *OrderService) SetCache(cache OrderCache) {
	s.cache = cache
}

// Save persists order, delegating to the repository directly.
func (s *OrderService) Save(ctx context.Context, order *domain.Order) error {
	return s.repo.Save(ctx, order)
}

// ListByCustomer returns a page of order summaries for customerID. List reads are not cached,
// since the pagination combinations make the key space unbounded.
func (s *OrderService) ListByCustomer(ctx context.Context, customerID uuid.UUID, limit, offset int) ([]*domain.Order, error) {
	return s.repo.ListByCustomer(ctx, customerID, limit, offset)
}

// GetByID returns the order with the given id for a read-only lookup. When a cache is
// configured, it is read first and filled on a miss; a cache error falls back to the repository
// rather than failing the read.
func (s *OrderService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Order, error) {
	if s.cache == nil {
		return s.repo.GetByID(ctx, id)
	}

	key := orderCacheKey(id)

	var cached domain.Order
	if hit, err := s.cache.GetJSON(ctx, key, &cached); err == nil && hit {
		return &cached, nil
	}

	order, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	_ = s.cache.SetJSON(ctx, key, order, orderCacheTTL)
	return order, nil
}

func orderCacheKey(id uuid.UUID) string {
	return "order:" + id.String()
}

// MarkPendingPaymentAfterCreate transitions order to pending_payment and enqueues
// order.ready_for_payment in its own transaction. It is used by the create-order handler, which
// persists the order separately through OrderRepository.Save and has no transaction to join.
func (s *OrderService) MarkPendingPaymentAfterCreate(ctx context.Context, orderID uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.MarkPendingPayment(ctx, tx, orderID); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkPendingPayment transitions order to pending_payment and enqueues order.ready_for_payment. Its
// saga is started and advanced through stock_reserved to awaiting_payment in the same transaction,
// since by the time this runs the order's stock is already reserved.
func (s *OrderService) MarkPendingPayment(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}

	if err := s.saga.Start(ctx, tx, orderID); err != nil {
		return err
	}
	if err := s.saga.Transition(ctx, tx, orderID, saga.StateStockReserved); err != nil {
		return err
	}
	if err := s.applyTransition(ctx, tx, order, (*domain.Order).MarkPendingPayment, events.EventTypeOrderReadyForPayment); err != nil {
		return err
	}
	return s.saga.Transition(ctx, tx, orderID, saga.StateAwaitingPayment)
}

// ConfirmPayment transitions order to confirmed and enqueues order.confirmed, advancing its saga
// from awaiting_payment through paid to completed. An order that already reached a terminal status
// is left untouched, since a redelivered or late payment event is not an error.
func (s *OrderService) ConfirmPayment(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if isTerminal(order.Status) {
		return nil
	}

	if err := s.saga.Transition(ctx, tx, orderID, saga.StatePaid); err != nil {
		return err
	}
	if err := s.applyTransition(ctx, tx, order, (*domain.Order).Confirm, events.EventTypeOrderConfirmed); err != nil {
		return err
	}
	return s.saga.Transition(ctx, tx, orderID, saga.StateCompleted)
}

// FailPayment compensates an order whose payment failed: it transitions the saga to compensating,
// releases the stock reservation, then transitions order to payment_failed and enqueues
// order.cancelled and the saga to compensated. The event catalog has no dedicated payment-failed
// type, and downstream consumers treat the order the same way as an explicit cancellation. An order
// that already reached a terminal status is left untouched, since a redelivered or late payment
// event is not an error.
//
// The compensating transition is committed in its own transaction before the reservation is
// released, so the saga durably records that compensation started even if the release fails and
// this method returns an error for the caller to retry. tx is used only for the final, successful
// transition, alongside order.cancelled and processed_events, atomically.
func (s *OrderService) FailPayment(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if isTerminal(order.Status) {
		return nil
	}

	if err := s.markCompensating(ctx, orderID); err != nil {
		return err
	}

	if err := s.inventory.Release(ctx, orderID); err != nil {
		_ = s.saga.SetLastError(ctx, orderID, err.Error())
		return fmt.Errorf("release reservation for order %s: %w", orderID, err)
	}

	if err := s.applyTransition(ctx, tx, order, (*domain.Order).Fail, events.EventTypeOrderCancelled); err != nil {
		return err
	}
	return s.saga.Transition(ctx, tx, orderID, saga.StateCompensated)
}

// FailAfterPayment compensates an order whose payment already succeeded but the order cannot be
// completed: it refunds the payment, releases the stock reservation, then cancels the order,
// undoing the saga's steps in the reverse order they ran, as on the compensation diagram. paymentID
// identifies the payment to refund; the caller is expected to know it, since the saga only reaches
// awaiting_payment before a payment result exists to record. An order that already reached a
// terminal status is left untouched.
//
// Like FailPayment, the compensating transition is committed in its own transaction before either
// compensating call, so the saga durably records that compensation started even if a step fails and
// this method returns an error for the caller to retry. tx is used only for the final, successful
// transition, alongside order.cancelled, atomically.
func (s *OrderService) FailAfterPayment(ctx context.Context, tx *sql.Tx, orderID, paymentID uuid.UUID, reason string) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if isTerminal(order.Status) {
		return nil
	}

	if err := s.markCompensating(ctx, orderID); err != nil {
		return err
	}

	if err := s.payments.Refund(ctx, paymentID, reason); err != nil {
		_ = s.saga.SetLastError(ctx, orderID, err.Error())
		return fmt.Errorf("refund payment %s for order %s: %w", paymentID, orderID, err)
	}
	if err := s.inventory.Release(ctx, orderID); err != nil {
		_ = s.saga.SetLastError(ctx, orderID, err.Error())
		return fmt.Errorf("release reservation for order %s: %w", orderID, err)
	}

	if err := s.applyTransition(ctx, tx, order, (*domain.Order).Cancel, events.EventTypeOrderCancelled); err != nil {
		return err
	}
	return s.saga.Transition(ctx, tx, orderID, saga.StateCompensated)
}

// markCompensating durably records that compensation for orderID has begun, in its own
// transaction, so the saga survives a later compensating step failing and the caller's own
// transaction rolling back.
func (s *OrderService) markCompensating(ctx context.Context, orderID uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.saga.Transition(ctx, tx, orderID, saga.StateCompensating); err != nil {
		return err
	}
	return tx.Commit()
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
