// Package cache implements a cache-aside helper over Redis, encoding values as JSON.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/database"
	"github.com/redis/go-redis/v9"
)

// DefaultTTL is used by SetJSON when called with ttl <= 0 and no other default was configured.
const DefaultTTL = 5 * time.Minute

// scanBatchSize bounds how many keys DeleteByPrefix asks Redis to return per SCAN call.
const scanBatchSize = 100

// Cache reads and writes JSON-encoded values in Redis, treating a missing key as a miss rather
// than an error.
type Cache struct {
	client     *database.RedisClient
	defaultTTL time.Duration
}

// New builds a Cache backed by client. defaultTTL is used by SetJSON whenever it is called with
// ttl <= 0; a non-positive defaultTTL falls back to DefaultTTL.
func New(client *database.RedisClient, defaultTTL time.Duration) *Cache {
	if defaultTTL <= 0 {
		defaultTTL = DefaultTTL
	}
	return &Cache{client: client, defaultTTL: defaultTTL}
}

// GetJSON reads key and unmarshals it into dest. A missing key is not an error: it reports a
// miss (false, nil) and leaves dest untouched.
func (c *Cache) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	raw, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get cache key %s: %w", key, err)
	}

	if err := json.Unmarshal(raw, dest); err != nil {
		return false, fmt.Errorf("unmarshal cache key %s: %w", key, err)
	}
	return true, nil
}

// SetJSON marshals value as JSON and stores it under key with ttl, or the cache's default TTL
// when ttl <= 0.
func (c *Cache) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal cache key %s: %w", key, err)
	}

	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("set cache key %s: %w", key, err)
	}
	return nil
}

// Delete removes keys. Deleting keys that do not exist is not an error.
func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	if err := c.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("delete cache keys %v: %w", keys, err)
	}
	return nil
}

// DeleteByPrefix removes every key starting with prefix, scanning the keyspace in batches
// instead of blocking Redis with KEYS.
func (c *Cache) DeleteByPrefix(ctx context.Context, prefix string) error {
	pattern := prefix + "*"

	var cursor uint64
	for {
		keys, next, err := c.client.Scan(ctx, cursor, pattern, scanBatchSize).Result()
		if err != nil {
			return fmt.Errorf("scan cache keys %s: %w", pattern, err)
		}

		if err := c.Delete(ctx, keys...); err != nil {
			return err
		}

		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}
