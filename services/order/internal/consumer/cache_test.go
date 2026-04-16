package consumer

import (
	"context"
	"errors"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

var errTestOrderCache = errors.New("cache failure")

// fakeOrderCache is an in-memory OrderCache test double.
type fakeOrderCache struct {
	deleted []string
	err     error
}

func (f *fakeOrderCache) Delete(_ context.Context, keys ...string) error {
	f.deleted = append(f.deleted, keys...)
	return f.err
}

func newCacheConsumer(t *testing.T, cache OrderCache) *CacheConsumer {
	t.Helper()
	return &CacheConsumer{cache: cache, logger: zaptest.NewLogger(t)}
}

func TestCacheConsumer_Handle(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
	}{
		{"order.created invalidates the order", events.EventTypeOrderCreated},
		{"order.ready_for_payment invalidates the order", events.EventTypeOrderReadyForPayment},
		{"order.confirmed invalidates the order", events.EventTypeOrderConfirmed},
		{"order.cancelled invalidates the order", events.EventTypeOrderCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orderID := uuid.New()
			event := newPaymentEvent(tt.eventType, orderID.String())

			cache := &fakeOrderCache{}
			c := newCacheConsumer(t, cache)

			if err := c.handle(context.Background(), event); err != nil {
				t.Fatalf("handle() error = %v", err)
			}
			want := "order:" + orderID.String()
			if len(cache.deleted) != 1 || cache.deleted[0] != want {
				t.Errorf("Delete calls = %v, want [%s]", cache.deleted, want)
			}
		})
	}
}

func TestCacheConsumer_Handle_MissingOrderID(t *testing.T) {
	event := events.Event{ID: uuid.New().String(), Type: events.EventTypeOrderCreated}

	cache := &fakeOrderCache{}
	c := newCacheConsumer(t, cache)

	if err := c.handle(context.Background(), event); err != nil {
		t.Fatalf("handle() error = %v", err)
	}
	if len(cache.deleted) != 0 {
		t.Errorf("Delete calls = %v, want none", cache.deleted)
	}
}

func TestCacheConsumer_Handle_CacheError(t *testing.T) {
	cache := &fakeOrderCache{err: errTestOrderCache}
	c := newCacheConsumer(t, cache)

	event := newPaymentEvent(events.EventTypeOrderCreated, uuid.New().String())

	if err := c.handle(context.Background(), event); !errors.Is(err, errTestOrderCache) {
		t.Errorf("handle() error = %v, want %v", err, errTestOrderCache)
	}
}

func TestCacheConsumer_Start(t *testing.T) {
	orderID := uuid.New()
	event := newPaymentEvent(events.EventTypeOrderConfirmed, orderID.String())

	cache := &fakeOrderCache{}
	c := newCacheConsumer(t, cache)
	c.subscriber = &fakeSubscriber{event: event}

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	want := "order:" + orderID.String()
	if len(cache.deleted) != 1 || cache.deleted[0] != want {
		t.Errorf("Delete calls = %v, want [%s]", cache.deleted, want)
	}
}
