// Package consumer handles inbound domain events for the payment service.
package consumer

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// PaymentProcessor is the port the orders consumer uses to charge an order once it is ready for
// payment.
type PaymentProcessor interface {
	ProcessPayment(ctx context.Context, orderID, customerID uuid.UUID, amountCents int64, currency string) (*domain.Payment, error)
}

// subscriber is the subset of *events.Subscriber used by OrdersConsumer, extracted so tests can
// substitute a fake.
type subscriber interface {
	Subscribe(ctx context.Context, handler func(context.Context, events.Event) error) error
}

// OrdersConsumer reacts to order.ready_for_payment by charging the order through payments.
//
// Unlike the order and inventory consumers, it cannot mark the event processed in the same
// transaction as the business write: ProcessPayment persists through the event store's own
// Repository.Save, which owns its transaction across the event stream, the read model and the
// outbox. The event is instead marked processed once ProcessPayment returns successfully;
// ProcessPayment's own idempotent lookup by order id covers a crash in the gap between the two.
type OrdersConsumer struct {
	subscriber subscriber
	db         *sql.DB
	processed  *events.ProcessedStore
	payments   PaymentProcessor
	logger     *zap.Logger
}

// NewOrdersConsumer builds an OrdersConsumer backed by sub, db, processed and payments.
func NewOrdersConsumer(sub *events.Subscriber, db *sql.DB, processed *events.ProcessedStore, payments PaymentProcessor, logger *zap.Logger) *OrdersConsumer {
	return &OrdersConsumer{subscriber: sub, db: db, processed: processed, payments: payments, logger: logger}
}

// Start consumes orders.events until ctx is cancelled or the subscriber fails.
func (c *OrdersConsumer) Start(ctx context.Context) error {
	return c.subscriber.Subscribe(ctx, c.handle)
}

// handle charges the order described by an order.ready_for_payment event. Other event types are
// ignored. A gateway decline (apperrors PAYMENT_FAILED) is a handled business outcome, not a
// reason to retry delivery: ProcessPayment already persisted the failed payment and its outbox
// event.
func (c *OrdersConsumer) handle(ctx context.Context, event events.Event) error {
	if event.Type != events.EventTypeOrderReadyForPayment {
		return nil
	}

	req, err := paymentRequestFromEvent(event)
	if err != nil {
		c.logger.Error("dropping order.ready_for_payment event with an invalid payload", zap.String("event_id", event.ID), zap.Error(err))
		return nil
	}

	wasProcessed, err := c.processed.WasProcessed(ctx, event.ID)
	if err != nil {
		return err
	}
	if wasProcessed {
		return nil
	}

	if _, err := c.payments.ProcessPayment(ctx, req.orderID, req.customerID, req.amountCents, req.currency); err != nil {
		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) || appErr.Code != "PAYMENT_FAILED" {
			return err
		}
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := c.processed.MarkProcessed(ctx, tx, event.ID, event.Type); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// paymentRequest is the data an order.ready_for_payment event carries to charge a payment.
type paymentRequest struct {
	orderID     uuid.UUID
	customerID  uuid.UUID
	amountCents int64
	currency    string
}

func paymentRequestFromEvent(event events.Event) (paymentRequest, error) {
	orderID, err := uuidFromData(event.Data, "order_id")
	if err != nil {
		return paymentRequest{}, fmt.Errorf("event %s: %w", event.ID, err)
	}
	customerID, err := uuidFromData(event.Data, "customer_id")
	if err != nil {
		return paymentRequest{}, fmt.Errorf("event %s: %w", event.ID, err)
	}
	amountCents, err := amountCentsFromData(event.Data, "total_amount_cents")
	if err != nil {
		return paymentRequest{}, fmt.Errorf("event %s: %w", event.ID, err)
	}
	currency, ok := event.Data["currency"].(string)
	if !ok {
		return paymentRequest{}, fmt.Errorf("event %s has no currency", event.ID)
	}

	return paymentRequest{orderID: orderID, customerID: customerID, amountCents: amountCents, currency: currency}, nil
}

func uuidFromData(data map[string]interface{}, key string) (uuid.UUID, error) {
	raw, ok := data[key]
	if !ok {
		return uuid.Nil, fmt.Errorf("has no %s", key)
	}
	str, ok := raw.(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("%s is not a string", key)
	}
	return uuid.Parse(str)
}

// amountCentsFromData reads key as the integer minor units (cents) it is stored as. Kafka events
// decode JSON numbers as float64, so the value is converted back to int64.
func amountCentsFromData(data map[string]interface{}, key string) (int64, error) {
	raw, ok := data[key]
	if !ok {
		return 0, fmt.Errorf("has no %s", key)
	}
	num, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("%s is not a number", key)
	}
	return int64(num), nil
}
