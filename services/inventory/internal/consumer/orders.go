// Package consumer handles inbound domain events for the inventory service.
package consumer

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// StockService is the port the orders consumer uses to react to order lifecycle events. It
// decides which event types it acts on; others are a no-op.
type StockService interface {
	HandleOrderEvent(ctx context.Context, tx *sql.Tx, eventType string, orderID uuid.UUID) error
}

// subscriber is the subset of *events.Subscriber used by OrdersConsumer, extracted so tests can
// substitute a fake.
type subscriber interface {
	Subscribe(ctx context.Context, handler func(events.Event) error) error
}

// OrdersConsumer applies order lifecycle events to stock reservations. Each event is handled
// inside a single transaction that also records it in processed_events, so redelivery is a no-op.
type OrdersConsumer struct {
	subscriber subscriber
	db         *sql.DB
	processed  *events.ProcessedStore
	stock      StockService
	logger     *zap.Logger
}

// NewOrdersConsumer builds an OrdersConsumer backed by sub, db, processed and stock.
func NewOrdersConsumer(sub *events.Subscriber, db *sql.DB, processed *events.ProcessedStore, stock StockService, logger *zap.Logger) *OrdersConsumer {
	return &OrdersConsumer{subscriber: sub, db: db, processed: processed, stock: stock, logger: logger}
}

// Start consumes orders.events until ctx is cancelled or the subscriber fails.
func (c *OrdersConsumer) Start(ctx context.Context) error {
	return c.subscriber.Subscribe(ctx, func(event events.Event) error {
		return c.handle(ctx, event)
	})
}

// handle applies event to the reservations of the order it references.
func (c *OrdersConsumer) handle(ctx context.Context, event events.Event) error {
	orderID, err := orderIDFromEvent(event)
	if err != nil {
		c.logger.Error("dropping order event with an invalid order id", zap.String("event_id", event.ID), zap.Error(err))
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

	if err := c.stock.HandleOrderEvent(ctx, tx, event.Type, orderID); err != nil {
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
