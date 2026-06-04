package cache

import (
	"context"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/database"
	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"
)

type sampleValue struct {
	Name string `json:"name"`
}

func newTestCache(t *testing.T) *Cache {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	return New(&database.RedisClient{Client: client}, time.Minute)
}

func TestCache_GetJSON_Miss(t *testing.T) {
	c := newTestCache(t)

	var dest sampleValue
	hit, err := c.GetJSON(context.Background(), "missing", &dest)
	if err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
	if hit {
		t.Fatal("GetJSON() hit = true, want false")
	}
}

func TestCache_SetJSON_GetJSON_Hit(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	want := sampleValue{Name: "widget"}
	if err := c.SetJSON(ctx, "key", want, time.Minute); err != nil {
		t.Fatalf("SetJSON() error = %v", err)
	}

	var got sampleValue
	hit, err := c.GetJSON(ctx, "key", &got)
	if err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
	if !hit {
		t.Fatal("GetJSON() hit = false, want true")
	}
	if got != want {
		t.Errorf("GetJSON() = %+v, want %+v", got, want)
	}
}

func TestCache_SetJSON_UsesDefaultTTL(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	if err := c.SetJSON(ctx, "key", sampleValue{Name: "widget"}, 0); err != nil {
		t.Fatalf("SetJSON() error = %v", err)
	}

	ttl, err := c.client.TTL(ctx, "key").Result()
	if err != nil {
		t.Fatalf("TTL() error = %v", err)
	}
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("TTL() = %v, want in (0, %v]", ttl, time.Minute)
	}
}

func TestCache_Delete(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	if err := c.SetJSON(ctx, "key", sampleValue{Name: "widget"}, time.Minute); err != nil {
		t.Fatalf("SetJSON() error = %v", err)
	}
	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	var got sampleValue
	hit, err := c.GetJSON(ctx, "key", &got)
	if err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
	if hit {
		t.Fatal("GetJSON() hit = true after delete, want false")
	}
}

func TestCache_Delete_NoKeys(t *testing.T) {
	c := newTestCache(t)

	if err := c.Delete(context.Background()); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}
}

func TestCache_GetJSON_RecordsHitAndMissMetrics(t *testing.T) {
	c := newTestCache(t)
	registry := prometheus.NewRegistry()
	m := NewMetrics(registry)
	c.SetMetrics(m, "order")
	ctx := context.Background()

	var dest sampleValue
	if _, err := c.GetJSON(ctx, "missing", &dest); err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
	if got := testutil.ToFloat64(m.misses.WithLabelValues("order")); got != 1 {
		t.Errorf("misses total = %v, want 1", got)
	}

	if err := c.SetJSON(ctx, "key", sampleValue{Name: "widget"}, time.Minute); err != nil {
		t.Fatalf("SetJSON() error = %v", err)
	}
	if _, err := c.GetJSON(ctx, "key", &dest); err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
	if got := testutil.ToFloat64(m.hits.WithLabelValues("order")); got != 1 {
		t.Errorf("hits total = %v, want 1", got)
	}
}

func TestCache_GetJSON_NoMetricsConfiguredDoesNotPanic(t *testing.T) {
	c := newTestCache(t)

	var dest sampleValue
	if _, err := c.GetJSON(context.Background(), "missing", &dest); err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
}

func TestCache_DeleteByPrefix(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	for _, key := range []string{"product:1", "product:2", "order:1"} {
		if err := c.SetJSON(ctx, key, sampleValue{Name: key}, time.Minute); err != nil {
			t.Fatalf("SetJSON(%s) error = %v", key, err)
		}
	}

	if err := c.DeleteByPrefix(ctx, "product:"); err != nil {
		t.Fatalf("DeleteByPrefix() error = %v", err)
	}

	wantHit := map[string]bool{"product:1": false, "product:2": false, "order:1": true}
	for key, want := range wantHit {
		var got sampleValue
		hit, err := c.GetJSON(ctx, key, &got)
		if err != nil {
			t.Fatalf("GetJSON(%s) error = %v", key, err)
		}
		if hit != want {
			t.Errorf("GetJSON(%s) hit = %v, want %v", key, hit, want)
		}
	}
}
