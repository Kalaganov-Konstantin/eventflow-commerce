package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid configuration",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":           "8080",
				"ORDER_DATABASE_URL":          "postgres://user:pass@localhost:5432/order?sslmode=disable",
				"REDIS_URL":                   "redis://localhost:6379",
				"KAFKA_BROKERS":               "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
				"INVENTORY_SERVICE_URL":       "http://inventory-service:8083",
				"PAYMENT_SERVICE_URL":         "http://payment-service:8082",
			},
			wantErr: false,
		},
		{
			name: "ORDER_SERVICE_PORT still works as fallback",
			envVars: map[string]string{
				"ORDER_SERVICE_PORT":          "8080",
				"ORDER_DATABASE_URL":          "postgres://user:pass@localhost:5432/order?sslmode=disable",
				"REDIS_URL":                   "redis://localhost:6379",
				"KAFKA_BROKERS":               "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
				"INVENTORY_SERVICE_URL":       "http://inventory-service:8083",
				"PAYMENT_SERVICE_URL":         "http://payment-service:8082",
			},
			wantErr: false,
		},
		{
			name: "Missing both port variables",
			envVars: map[string]string{
				"ORDER_DATABASE_URL":          "postgres://user:pass@localhost:5432/order?sslmode=disable",
				"REDIS_URL":                   "redis://localhost:6379",
				"KAFKA_BROKERS":               "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "ORDER_SERVER_PORT (or ORDER_SERVICE_PORT) environment variable is not set",
		},
		{
			name: "Missing ORDER_DATABASE_URL",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":           "8080",
				"REDIS_URL":                   "redis://localhost:6379",
				"KAFKA_BROKERS":               "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "ORDER_DATABASE_URL environment variable is not set",
		},
		{
			name: "Invalid database URL",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":           "8080",
				"ORDER_DATABASE_URL":          "invalid-url",
				"REDIS_URL":                   "redis://localhost:6379",
				"KAFKA_BROKERS":               "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
				"INVENTORY_SERVICE_URL":       "http://inventory-service:8083",
				"PAYMENT_SERVICE_URL":         "http://payment-service:8082",
			},
			wantErr: true,
			errMsg:  "database host is required",
		},
		{
			name: "Missing REDIS_URL",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":           "8080",
				"ORDER_DATABASE_URL":          "postgres://user:pass@localhost:5432/order?sslmode=disable",
				"KAFKA_BROKERS":               "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "REDIS_URL environment variable is not set",
		},
		{
			name: "Missing INVENTORY_SERVICE_URL",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":           "8080",
				"ORDER_DATABASE_URL":          "postgres://user:pass@localhost:5432/order?sslmode=disable",
				"REDIS_URL":                   "redis://localhost:6379",
				"KAFKA_BROKERS":               "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "INVENTORY_SERVICE_URL environment variable is not set",
		},
		{
			name: "Missing PAYMENT_SERVICE_URL",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":           "8080",
				"ORDER_DATABASE_URL":          "postgres://user:pass@localhost:5432/order?sslmode=disable",
				"REDIS_URL":                   "redis://localhost:6379",
				"KAFKA_BROKERS":               "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
				"INVENTORY_SERVICE_URL":       "http://inventory-service:8083",
			},
			wantErr: true,
			errMsg:  "PAYMENT_SERVICE_URL environment variable is not set",
		},
		{
			name: "Missing KAFKA_BROKERS",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":           "8080",
				"ORDER_DATABASE_URL":          "postgres://user:pass@localhost:5432/order?sslmode=disable",
				"REDIS_URL":                   "redis://localhost:6379",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
				"INVENTORY_SERVICE_URL":       "http://inventory-service:8083",
				"PAYMENT_SERVICE_URL":         "http://payment-service:8082",
			},
			wantErr: true,
			errMsg:  "KAFKA_BROKERS environment variable is not set",
		},
		{
			name: "Missing OTEL_EXPORTER_OTLP_ENDPOINT",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":     "8080",
				"ORDER_DATABASE_URL":    "postgres://user:pass@localhost:5432/order?sslmode=disable",
				"REDIS_URL":             "redis://localhost:6379",
				"KAFKA_BROKERS":         "localhost:9092",
				"INVENTORY_SERVICE_URL": "http://inventory-service:8083",
				"PAYMENT_SERVICE_URL":   "http://payment-service:8082",
			},
			wantErr: true,
			errMsg:  "OTEL_EXPORTER_OTLP_ENDPOINT environment variable is not set",
		},
		{
			name: "Unparsable database URL",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":           "8080",
				"ORDER_DATABASE_URL":          "postgres://%zzhost:5432/order",
				"REDIS_URL":                   "redis://localhost:6379",
				"KAFKA_BROKERS":               "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:14268/api/traces",
				"INVENTORY_SERVICE_URL":       "http://inventory-service:8083",
				"PAYMENT_SERVICE_URL":         "http://payment-service:8082",
			},
			wantErr: true,
			errMsg:  "invalid ORDER_DATABASE_URL",
		},
		{
			name: "Unparsable numeric override",
			envVars: map[string]string{
				"ORDER_SERVER_PORT":                  "8080",
				"ORDER_DATABASE_URL":                 "postgres://user:pass@localhost:5432/order?sslmode=disable",
				"REDIS_URL":                          "redis://localhost:6379",
				"KAFKA_BROKERS":                      "localhost:9092",
				"OTEL_EXPORTER_OTLP_ENDPOINT":        "http://localhost:14268/api/traces",
				"INVENTORY_SERVICE_URL":              "http://inventory-service:8083",
				"PAYMENT_SERVICE_URL":                "http://payment-service:8082",
				"ORDER_DATABASE_POOL_MAX_OPEN_CONNS": "not-a-number",
			},
			wantErr: true,
			errMsg:  "error unmarshaling config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars first
			clearEnvVars()

			// Set test env vars
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			cfg, err := LoadConfig()

			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadConfig() expected error but got none")
					return
				}
				if tt.errMsg != "" {
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("LoadConfig() error = %v, want error containing %v", err, tt.errMsg)
					}
				}
			} else {
				if err != nil {
					t.Errorf("LoadConfig() unexpected error = %v", err)
					return
				}
				if cfg == nil {
					t.Errorf("LoadConfig() returned nil config")
					return
				}

				// Verify configuration was loaded correctly
				if cfg.Server.Port != "8080" {
					t.Errorf("LoadConfig() Server.Port = %v, want 8080", cfg.Server.Port)
				}
				if cfg.Database.Host != "localhost" {
					t.Errorf("LoadConfig() Database.Host = %v, want localhost", cfg.Database.Host)
				}
				if cfg.Kafka.GroupID != "order-service" {
					t.Errorf("LoadConfig() Kafka.GroupID = %v, want order-service", cfg.Kafka.GroupID)
				}
				if cfg.Kafka.DLQTopic != "orders.events.dlq" {
					t.Errorf("LoadConfig() Kafka.DLQTopic = %v, want orders.events.dlq", cfg.Kafka.DLQTopic)
				}
				if cfg.Service.Name != "order" {
					t.Errorf("LoadConfig() Service.Name = %v, want order", cfg.Service.Name)
				}
				if cfg.Logger.Level != "info" {
					t.Errorf("LoadConfig() Logger.Level = %v, want info", cfg.Logger.Level)
				}
				if cfg.DatabaseURL != tt.envVars["ORDER_DATABASE_URL"] {
					t.Errorf("LoadConfig() DatabaseURL = %v, want %v", cfg.DatabaseURL, tt.envVars["ORDER_DATABASE_URL"])
				}
				if cfg.DatabasePool.MaxOpenConns != 25 {
					t.Errorf("LoadConfig() DatabasePool.MaxOpenConns = %v, want 25", cfg.DatabasePool.MaxOpenConns)
				}
				if cfg.DatabasePool.MaxIdleConns != 5 {
					t.Errorf("LoadConfig() DatabasePool.MaxIdleConns = %v, want 5", cfg.DatabasePool.MaxIdleConns)
				}
				if cfg.DatabasePool.MaxLifetime != 5*time.Minute {
					t.Errorf("LoadConfig() DatabasePool.MaxLifetime = %v, want 5m", cfg.DatabasePool.MaxLifetime)
				}
				if cfg.Outbox.RelayInterval != time.Second {
					t.Errorf("LoadConfig() Outbox.RelayInterval = %v, want 1s", cfg.Outbox.RelayInterval)
				}
				if cfg.Outbox.RelayBatchSize != 100 {
					t.Errorf("LoadConfig() Outbox.RelayBatchSize = %v, want 100", cfg.Outbox.RelayBatchSize)
				}
				if cfg.InventoryServiceURL != tt.envVars["INVENTORY_SERVICE_URL"] {
					t.Errorf("LoadConfig() InventoryServiceURL = %v, want %v", cfg.InventoryServiceURL, tt.envVars["INVENTORY_SERVICE_URL"])
				}
				if cfg.InventoryClient.Timeout != 5*time.Second {
					t.Errorf("LoadConfig() InventoryClient.Timeout = %v, want 5s", cfg.InventoryClient.Timeout)
				}
				if cfg.PaymentServiceURL != tt.envVars["PAYMENT_SERVICE_URL"] {
					t.Errorf("LoadConfig() PaymentServiceURL = %v, want %v", cfg.PaymentServiceURL, tt.envVars["PAYMENT_SERVICE_URL"])
				}
				if cfg.PaymentClient.Timeout != 5*time.Second {
					t.Errorf("LoadConfig() PaymentClient.Timeout = %v, want 5s", cfg.PaymentClient.Timeout)
				}
			}

			// Clean up
			clearEnvVars()
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid config",
			config: Config{
				Server: config.ServerConfig{
					Host: "0.0.0.0",
					Port: "8080",
				},
				Database: config.DatabaseConfig{
					Host:     "localhost",
					Port:     "5432",
					User:     "user",
					Password: "pass",
					DBName:   "order",
					SSLMode:  "disable",
				},
				Redis: config.RedisConfig{
					URL: "redis://localhost:6379",
				},
				Kafka: config.KafkaConfig{
					Brokers: []string{"localhost:9092"},
				},
				Jaeger: config.JaegerConfig{
					Endpoint: "http://localhost:14268/api/traces",
				},
				InventoryServiceURL: "http://inventory:8080",
				PaymentServiceURL:   "http://payment:8080",
			},
			wantErr: false,
		},
		{
			name: "Missing server port",
			config: Config{
				Redis: config.RedisConfig{
					URL: "redis://localhost:6379",
				},
				Kafka: config.KafkaConfig{
					Brokers: []string{"localhost:9092"},
				},
				Jaeger: config.JaegerConfig{
					Endpoint: "http://localhost:14268/api/traces",
				},
			},
			wantErr: true,
			errMsg:  "ORDER_SERVER_PORT (or ORDER_SERVICE_PORT) environment variable is not set",
		},
		{
			name: "Missing kafka brokers",
			config: Config{
				Server: config.ServerConfig{Port: "8080"},
				Redis:  config.RedisConfig{URL: "redis://localhost:6379"},
				Jaeger: config.JaegerConfig{Endpoint: "http://localhost:14268/api/traces"},
			},
			wantErr: true,
			errMsg:  "KAFKA_BROKERS environment variable is not set",
		},
		{
			name: "Missing jaeger endpoint",
			config: Config{
				Server: config.ServerConfig{Port: "8080"},
				Redis:  config.RedisConfig{URL: "redis://localhost:6379"},
				Kafka:  config.KafkaConfig{Brokers: []string{"localhost:9092"}},
			},
			wantErr: true,
			errMsg:  "OTEL_EXPORTER_OTLP_ENDPOINT environment variable is not set",
		},
		{
			name: "Missing database port",
			config: Config{
				Server:              config.ServerConfig{Port: "8080"},
				Database:            config.DatabaseConfig{Host: "localhost"},
				Redis:               config.RedisConfig{URL: "redis://localhost:6379"},
				Kafka:               config.KafkaConfig{Brokers: []string{"localhost:9092"}},
				Jaeger:              config.JaegerConfig{Endpoint: "http://localhost:14268/api/traces"},
				InventoryServiceURL: "http://inventory:8080",
				PaymentServiceURL:   "http://payment:8080",
			},
			wantErr: true,
			errMsg:  "database port is required in ORDER_DATABASE_URL",
		},
		{
			name: "Missing database user",
			config: Config{
				Server:              config.ServerConfig{Port: "8080"},
				Database:            config.DatabaseConfig{Host: "localhost", Port: "5432"},
				Redis:               config.RedisConfig{URL: "redis://localhost:6379"},
				Kafka:               config.KafkaConfig{Brokers: []string{"localhost:9092"}},
				Jaeger:              config.JaegerConfig{Endpoint: "http://localhost:14268/api/traces"},
				InventoryServiceURL: "http://inventory:8080",
				PaymentServiceURL:   "http://payment:8080",
			},
			wantErr: true,
			errMsg:  "database user is required in ORDER_DATABASE_URL",
		},
		{
			name: "Missing database password",
			config: Config{
				Server:              config.ServerConfig{Port: "8080"},
				Database:            config.DatabaseConfig{Host: "localhost", Port: "5432", User: "user"},
				Redis:               config.RedisConfig{URL: "redis://localhost:6379"},
				Kafka:               config.KafkaConfig{Brokers: []string{"localhost:9092"}},
				Jaeger:              config.JaegerConfig{Endpoint: "http://localhost:14268/api/traces"},
				InventoryServiceURL: "http://inventory:8080",
				PaymentServiceURL:   "http://payment:8080",
			},
			wantErr: true,
			errMsg:  "database password is required in ORDER_DATABASE_URL",
		},
		{
			name: "Missing database name",
			config: Config{
				Server:              config.ServerConfig{Port: "8080"},
				Database:            config.DatabaseConfig{Host: "localhost", Port: "5432", User: "user", Password: "pass"},
				Redis:               config.RedisConfig{URL: "redis://localhost:6379"},
				Kafka:               config.KafkaConfig{Brokers: []string{"localhost:9092"}},
				Jaeger:              config.JaegerConfig{Endpoint: "http://localhost:14268/api/traces"},
				InventoryServiceURL: "http://inventory:8080",
				PaymentServiceURL:   "http://payment:8080",
			},
			wantErr: true,
			errMsg:  "database name is required in ORDER_DATABASE_URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %v, want %v", err, tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func clearEnvVars() {
	envVars := []string{
		"ORDER_SERVER_PORT",
		"ORDER_SERVICE_PORT",
		"ORDER_DATABASE_URL",
		"REDIS_URL",
		"KAFKA_BROKERS",
		"ORDER_KAFKA_GROUP_ID",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"INVENTORY_SERVICE_URL",
		"PAYMENT_SERVICE_URL",
		"ORDER_DATABASE_POOL_MAX_OPEN_CONNS",
	}
	for _, env := range envVars {
		os.Unsetenv(env)
	}
}
