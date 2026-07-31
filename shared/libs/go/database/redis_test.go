package database

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestLoadRedisConfig_Defaults(t *testing.T) {
	cfg, err := LoadRedisConfig()
	if err != nil {
		t.Fatalf("LoadRedisConfig() error = %v", err)
	}

	if cfg.URL != "redis://localhost:6379/0" {
		t.Errorf("URL = %q, want the default connection string", cfg.URL)
	}
	if cfg.PoolSize != 10 {
		t.Errorf("PoolSize = %d, want 10", cfg.PoolSize)
	}
}

func TestLoadRedisConfig_EnvOverrides(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://cache:6379/1")
	t.Setenv("REDIS_POOL_SIZE", "20")

	cfg, err := LoadRedisConfig()
	if err != nil {
		t.Fatalf("LoadRedisConfig() error = %v", err)
	}

	if cfg.URL != "redis://cache:6379/1" {
		t.Errorf("URL = %q, want the env value", cfg.URL)
	}
	if cfg.PoolSize != 20 {
		t.Errorf("PoolSize = %d, want 20", cfg.PoolSize)
	}
}

func TestLoadRedisConfig_ReturnsErrorForUnparsableEnvValue(t *testing.T) {
	t.Setenv("REDIS_POOL_SIZE", "not-an-int")

	if _, err := LoadRedisConfig(); err == nil {
		t.Fatal("LoadRedisConfig() error = nil, want error for a non-numeric REDIS_POOL_SIZE")
	}
}

func TestNewRedisConnection_ReturnsErrorForInvalidURL(t *testing.T) {
	_, err := NewRedisConnection(RedisConfig{URL: "not-a-valid-url"})
	if err == nil {
		t.Fatal("NewRedisConnection() error = nil, want a URL parse error")
	}
}

func TestNewRedisConnection_ReturnsErrorWhenPingFails(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()

	_, err := NewRedisConnection(RedisConfig{URL: "redis://" + addr, PoolSize: 1})
	if err == nil {
		t.Fatal("NewRedisConnection() error = nil, want error once the server is gone")
	}
}

func TestNewRedisConnection_Success(t *testing.T) {
	mr := miniredis.RunT(t)

	rc, err := NewRedisConnection(RedisConfig{URL: "redis://" + mr.Addr(), PoolSize: 7})
	if err != nil {
		t.Fatalf("NewRedisConnection() error = %v", err)
	}

	if got := rc.Options().PoolSize; got != 7 {
		t.Errorf("PoolSize = %d, want 7", got)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
