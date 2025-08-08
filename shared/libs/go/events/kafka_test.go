package events

import "testing"

func TestLoadKafkaConfig_MultipleBrokers(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", "a:9092,b:9092")

	cfg, err := LoadKafkaConfig()
	if err != nil {
		t.Fatalf("LoadKafkaConfig() error = %v", err)
	}

	want := []string{"a:9092", "b:9092"}
	if len(cfg.Brokers) != len(want) {
		t.Fatalf("Brokers = %#v, want %#v", cfg.Brokers, want)
	}
	for i, broker := range want {
		if cfg.Brokers[i] != broker {
			t.Errorf("Brokers[%d] = %q, want %q", i, cfg.Brokers[i], broker)
		}
	}
}

func TestLoadKafkaConfig_DefaultsWithoutEnv(t *testing.T) {
	cfg, err := LoadKafkaConfig()
	if err != nil {
		t.Fatalf("LoadKafkaConfig() error = %v", err)
	}

	if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "localhost:9092" {
		t.Errorf("Brokers = %#v, want [localhost:9092]", cfg.Brokers)
	}
	if cfg.GroupID != "eventflow-service" {
		t.Errorf("GroupID = %q, want %q", cfg.GroupID, "eventflow-service")
	}
	if cfg.DLQTopic != "eventflow-dlq" {
		t.Errorf("DLQTopic = %q, want %q", cfg.DLQTopic, "eventflow-dlq")
	}
}

func TestLoadKafkaConfig_GroupIDAndDLQTopicFromEnv(t *testing.T) {
	t.Setenv("KAFKA_GROUP_ID", "order-service")
	t.Setenv("KAFKA_DLQ_TOPIC", "orders.events.dlq")

	cfg, err := LoadKafkaConfig()
	if err != nil {
		t.Fatalf("LoadKafkaConfig() error = %v", err)
	}

	if cfg.GroupID != "order-service" {
		t.Errorf("GroupID = %q, want %q", cfg.GroupID, "order-service")
	}
	if cfg.DLQTopic != "orders.events.dlq" {
		t.Errorf("DLQTopic = %q, want %q", cfg.DLQTopic, "orders.events.dlq")
	}
}
