package consumer

import (
	"context"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"go.uber.org/zap"
)

// ProductCache is the invalidation port the cache consumer deletes keys through.
type ProductCache interface {
	Delete(ctx context.Context, keys ...string) error
}

// CacheConsumer invalidates cached product reads when inventory.events reports a change:
// product.updated, inventory.reserved or inventory.released.
type CacheConsumer struct {
	subscriber subscriber
	cache      ProductCache
	logger     *zap.Logger
}

// NewCacheConsumer builds a CacheConsumer backed by sub and cache.
func NewCacheConsumer(sub *events.Subscriber, cache ProductCache, logger *zap.Logger) *CacheConsumer {
	return &CacheConsumer{subscriber: sub, cache: cache, logger: logger}
}

// Start consumes inventory.events until ctx is cancelled or the subscriber fails.
func (c *CacheConsumer) Start(ctx context.Context) error {
	return c.subscriber.Subscribe(ctx, func(event events.Event) error {
		return c.handle(ctx, event)
	})
}

// handle deletes the cached product entries affected by event. Event types this consumer does
// not act on, and events with no recognizable product id, are skipped without error.
func (c *CacheConsumer) handle(ctx context.Context, event events.Event) error {
	productIDs := productIDsFromEvent(event)
	if len(productIDs) == 0 {
		return nil
	}

	keys := make([]string, len(productIDs))
	for i, id := range productIDs {
		keys[i] = "product:" + id
	}

	if err := c.cache.Delete(ctx, keys...); err != nil {
		return fmt.Errorf("invalidate product cache: %w", err)
	}
	return nil
}

// productIDsFromEvent extracts the product ids affected by event, based on its type.
func productIDsFromEvent(event events.Event) []string {
	switch event.Type {
	case events.EventTypeProductUpdated:
		if id, ok := event.Data["product_id"].(string); ok {
			return []string{id}
		}
		return nil
	case events.EventTypeInventoryReserved, events.EventTypeInventoryReleased:
		return productIDsFromItems(event.Data["items"])
	default:
		return nil
	}
}

// productIDsFromItems extracts product ids from an inventory event's items payload.
func productIDsFromItems(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}

	ids := make([]string, 0, len(items))
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := fields["product_id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}
