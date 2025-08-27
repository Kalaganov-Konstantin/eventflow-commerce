package config

import "testing"

func TestRedisConfig_URLParsedThroughViper(t *testing.T) {
	loader := New("redis_types_service")
	loader.SetDefault("redis.url", "redis://localhost:6379/0")

	var cfg struct {
		Redis RedisConfig `mapstructure:"redis"`
	}
	if err := loader.Load(&cfg); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Redis.URL != "redis://localhost:6379/0" {
		t.Errorf("cfg.Redis.URL = %q, want %q", cfg.Redis.URL, "redis://localhost:6379/0")
	}
}

func TestKafkaConfig_GroupIDAndDLQTopicParsedThroughViper(t *testing.T) {
	loader := New("kafka_types_service")
	loader.SetDefault("kafka.group_id", "order-service")
	loader.SetDefault("kafka.dlq_topic", "orders.events.dlq")

	var cfg struct {
		Kafka KafkaConfig `mapstructure:"kafka"`
	}
	if err := loader.Load(&cfg); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Kafka.GroupID != "order-service" {
		t.Errorf("cfg.Kafka.GroupID = %q, want %q", cfg.Kafka.GroupID, "order-service")
	}
	if cfg.Kafka.DLQTopic != "orders.events.dlq" {
		t.Errorf("cfg.Kafka.DLQTopic = %q, want %q", cfg.Kafka.DLQTopic, "orders.events.dlq")
	}
}
