package database

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

type RedisConfig struct {
	URL      string `mapstructure:"REDIS_URL"`
	PoolSize int    `mapstructure:"REDIS_POOL_SIZE"`
}

type RedisClient struct {
	*redis.Client
}

func NewRedisConnection(config RedisConfig) (*RedisClient, error) {
	opts, err := redis.ParseURL(config.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}
	opts.PoolSize = config.PoolSize

	rdb := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisClient{Client: rdb}, nil
}

func (r *RedisClient) Close() error {
	return r.Client.Close()
}

func LoadRedisConfig() (RedisConfig, error) {
	v := viper.New()
	v.AutomaticEnv()

	v.SetDefault("REDIS_URL", "redis://localhost:6379/0")
	v.SetDefault("REDIS_POOL_SIZE", 10)

	var config RedisConfig
	if err := v.Unmarshal(&config); err != nil {
		return RedisConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return config, nil
}
