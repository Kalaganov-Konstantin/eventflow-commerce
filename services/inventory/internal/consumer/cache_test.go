package consumer

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

var errTestProductCache = errors.New("cache failure")

// fakeProductCache is an in-memory ProductCache test double.
type fakeProductCache struct {
	deleted [][]string
	err     error
}

func (f *fakeProductCache) Delete(_ context.Context, keys ...string) error {
	f.deleted = append(f.deleted, keys)
	return f.err
}

func newCacheConsumer(t *testing.T, cache ProductCache) *CacheConsumer {
	t.Helper()
	return &CacheConsumer{cache: cache, logger: zaptest.NewLogger(t)}
}

func TestCacheConsumer_Handle(t *testing.T) {
	tests := []struct {
		name  string
		event events.Event
		want  []string
	}{
		{
			name: "product.updated invalidates the product",
			event: events.Event{
				ID:   uuid.New().String(),
				Type: events.EventTypeProductUpdated,
				Data: map[string]interface{}{"product_id": "p1"},
			},
			want: []string{"product:p1"},
		},
		{
			name: "inventory.reserved invalidates every reserved product",
			event: events.Event{
				ID:   uuid.New().String(),
				Type: events.EventTypeInventoryReserved,
				Data: map[string]interface{}{
					"order_id": uuid.New().String(),
					"items": []interface{}{
						map[string]interface{}{"product_id": "p1", "quantity": float64(2)},
						map[string]interface{}{"product_id": "p2", "quantity": float64(1)},
					},
				},
			},
			want: []string{"product:p1", "product:p2"},
		},
		{
			name: "inventory.released invalidates every released product",
			event: events.Event{
				ID:   uuid.New().String(),
				Type: events.EventTypeInventoryReleased,
				Data: map[string]interface{}{
					"order_id": uuid.New().String(),
					"items": []interface{}{
						map[string]interface{}{"product_id": "p3", "quantity": float64(1)},
					},
				},
			},
			want: []string{"product:p3"},
		},
		{
			name: "unrelated event type is ignored",
			event: events.Event{
				ID:   uuid.New().String(),
				Type: events.EventTypeOrderCreated,
				Data: map[string]interface{}{"order_id": uuid.New().String()},
			},
			want: nil,
		},
		{
			name: "product.updated with no product id is ignored",
			event: events.Event{
				ID:   uuid.New().String(),
				Type: events.EventTypeProductUpdated,
				Data: map[string]interface{}{},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &fakeProductCache{}
			c := newCacheConsumer(t, cache)

			if err := c.handle(context.Background(), tt.event); err != nil {
				t.Fatalf("handle() error = %v", err)
			}

			if tt.want == nil {
				if len(cache.deleted) != 0 {
					t.Errorf("Delete calls = %v, want none", cache.deleted)
				}
				return
			}

			if len(cache.deleted) != 1 {
				t.Fatalf("Delete calls = %v, want exactly one", cache.deleted)
			}
			got := append([]string(nil), cache.deleted[0]...)
			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Delete keys = %v, want %v", got, want)
			}
		})
	}
}

func TestCacheConsumer_Handle_CacheError(t *testing.T) {
	cache := &fakeProductCache{err: errTestProductCache}
	c := newCacheConsumer(t, cache)

	event := events.Event{
		ID:   uuid.New().String(),
		Type: events.EventTypeProductUpdated,
		Data: map[string]interface{}{"product_id": "p1"},
	}

	if err := c.handle(context.Background(), event); !errors.Is(err, errTestProductCache) {
		t.Errorf("handle() error = %v, want %v", err, errTestProductCache)
	}
}

func TestCacheConsumer_Start(t *testing.T) {
	event := events.Event{
		ID:   uuid.New().String(),
		Type: events.EventTypeProductUpdated,
		Data: map[string]interface{}{"product_id": "p1"},
	}

	cache := &fakeProductCache{}
	c := newCacheConsumer(t, cache)
	c.subscriber = &fakeSubscriber{event: event}

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(cache.deleted) != 1 || cache.deleted[0][0] != "product:p1" {
		t.Errorf("Delete calls = %v, want one call for product:p1", cache.deleted)
	}
}
