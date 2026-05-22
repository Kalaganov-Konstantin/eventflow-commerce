// Package consumer handles inbound domain events for the order service.
package consumer

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	sharedlogger "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// OrderService is the port the payments consumer uses to react to payment results.
type OrderService interface {
	ConfirmPayment(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error
	FailPayment(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error
}

// subscriber is the subset of *events.Subscriber used by PaymentsConsumer, extracted so tests
// can substitute a fake.
type subscriber interface {
	Subscribe(ctx context.Context, handler func(context.Context, events.Event) error) error
}

// PaymentsConsumer applies payment results to orders. Each event is handled inside a single
// transaction that also records it in processed_events, so redelivery is a no-op.
type PaymentsConsumer struct {
	subscriber subscriber
	db         *sql.DB
	processed  *events.ProcessedStore
	orders     OrderService
	logger     *sharedlogger.Logger
}

// NewPaymentsConsumer builds a PaymentsConsumer backed by sub, db, processed and orders.
func NewPaymentsConsumer(sub *events.Subscriber, db *sql.DB, processed *events.ProcessedStore, orders OrderService, logger *sharedlogger.Logger) *PaymentsConsumer {
	return &PaymentsConsumer{subscriber: sub, db: db, processed: processed, orders: orders, logger: logger}
}

// Start consumes payments.events until ctx is cancelled or the subscriber fails.
func (c *PaymentsConsumer) Start(ctx context.Context) error {
	return c.subscriber.Subscribe(ctx, c.handle)
}

// handle applies event to the order it references. Unknown event types are skipped without
// error, since the topic may carry payment events this consumer does not act on.
func (c *PaymentsConsumer) handle(ctx context.Context, event events.Event) error {
	if event.Type != events.EventTypePaymentProcessed && event.Type != events.EventTypePaymentFailed {
		return nil
	}

	orderID, err := orderIDFromEvent(event)
	if err != nil {
		c.logger.WithTracing(ctx).Error("dropping payment event with an invalid order id", zap.String("event_id", event.ID), zap.Error(err))
		return nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	processed, err := c.processed.MarkProcessed(ctx, tx, event.ID, event.Type)
	if err != nil {
		return err
	}
	if !processed {
		return tx.Commit() // already handled, redelivery is a no-op
	}

	switch event.Type {
	case events.EventTypePaymentProcessed:
		err = c.orders.ConfirmPayment(ctx, tx, orderID)
	case events.EventTypePaymentFailed:
		err = c.orders.FailPayment(ctx, tx, orderID)
	}
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func orderIDFromEvent(event events.Event) (uuid.UUID, error) {
	raw, ok := event.Data["order_id"]
	if !ok {
		return uuid.Nil, fmt.Errorf("event %s has no order_id", event.ID)
	}
	str, ok := raw.(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("event %s order_id is not a string", event.ID)
	}
	return uuid.Parse(str)
}
