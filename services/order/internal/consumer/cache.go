package consumer

import (
	"context"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"go.uber.org/zap"
)

// OrderCache is the invalidation port the cache consumer deletes keys through.
type OrderCache interface {
	Delete(ctx context.Context, keys ...string) error
}

// CacheConsumer invalidates the cached order read whenever orders.events reports a status
// change for it.
type CacheConsumer struct {
	subscriber subscriber
	cache      OrderCache
	logger     *zap.Logger
}

// NewCacheConsumer builds a CacheConsumer backed by sub and cache.
func NewCacheConsumer(sub *events.Subscriber, cache OrderCache, logger *zap.Logger) *CacheConsumer {
	return &CacheConsumer{subscriber: sub, cache: cache, logger: logger}
}

// Start consumes orders.events until ctx is cancelled or the subscriber fails.
func (c *CacheConsumer) Start(ctx context.Context) error {
	return c.subscriber.Subscribe(ctx, c.handle)
}

// handle deletes the cached order entry affected by event. An event with no recognizable order
// id is skipped without error.
func (c *CacheConsumer) handle(ctx context.Context, event events.Event) error {
	orderID, err := orderIDFromEvent(event)
	if err != nil {
		c.logger.Error("dropping order event with an invalid order id", zap.String("event_id", event.ID), zap.Error(err))
		return nil
	}

	if err := c.cache.Delete(ctx, "order:"+orderID.String()); err != nil {
		return fmt.Errorf("invalidate order cache: %w", err)
	}
	return nil
}
