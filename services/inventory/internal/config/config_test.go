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
				"INVENTORY_SERVER_PORT":  "8080",
				"INVENTORY_DATABASE_URL": "postgres://user:pass@localhost:5432/inventory?sslmode=disable",
				"REDIS_URL":              "redis://localhost:6379",
				"KAFKA_BROKERS":          "localhost:9092",
				"JAEGER_ENDPOINT":        "http://localhost:14268/api/traces",
			},
			wantErr: false,
		},
		{
			name: "INVENTORY_SERVICE_PORT still works as fallback",
			envVars: map[string]string{
				"INVENTORY_SERVICE_PORT": "8080",
				"INVENTORY_DATABASE_URL": "postgres://user:pass@localhost:5432/inventory?sslmode=disable",
				"REDIS_URL":              "redis://localhost:6379",
				"KAFKA_BROKERS":          "localhost:9092",
				"JAEGER_ENDPOINT":        "http://localhost:14268/api/traces",
			},
			wantErr: false,
		},
		{
			name: "Missing both port variables",
			envVars: map[string]string{
				"INVENTORY_DATABASE_URL": "postgres://user:pass@localhost:5432/inventory?sslmode=disable",
				"REDIS_URL":              "redis://localhost:6379",
				"KAFKA_BROKERS":          "localhost:9092",
				"JAEGER_ENDPOINT":        "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "INVENTORY_SERVER_PORT (or INVENTORY_SERVICE_PORT) environment variable is not set",
		},
		{
			name: "Missing INVENTORY_DATABASE_URL",
			envVars: map[string]string{
				"INVENTORY_SERVER_PORT": "8080",
				"REDIS_URL":             "redis://localhost:6379",
				"KAFKA_BROKERS":         "localhost:9092",
				"JAEGER_ENDPOINT":       "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "INVENTORY_DATABASE_URL environment variable is not set",
		},
		{
			name: "Invalid database URL",
			envVars: map[string]string{
				"INVENTORY_SERVER_PORT":  "8080",
				"INVENTORY_DATABASE_URL": "invalid-url",
				"REDIS_URL":              "redis://localhost:6379",
				"KAFKA_BROKERS":          "localhost:9092",
				"JAEGER_ENDPOINT":        "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "database host is required",
		},
		{
			name: "Missing Redis URL",
			envVars: map[string]string{
				"INVENTORY_SERVER_PORT":  "8080",
				"INVENTORY_DATABASE_URL": "postgres://user:pass@localhost:5432/inventory?sslmode=disable",
				"KAFKA_BROKERS":          "localhost:9092",
				"JAEGER_ENDPOINT":        "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "REDIS_URL environment variable is not set",
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
				if cfg.Kafka.GroupID != "inventory-service" {
					t.Errorf("LoadConfig() Kafka.GroupID = %v, want inventory-service", cfg.Kafka.GroupID)
				}
				if cfg.Kafka.DLQTopic != "inventory.events.dlq" {
					t.Errorf("LoadConfig() Kafka.DLQTopic = %v, want inventory.events.dlq", cfg.Kafka.DLQTopic)
				}
				if cfg.Service.Name != "inventory" {
					t.Errorf("LoadConfig() Service.Name = %v, want inventory", cfg.Service.Name)
				}
				if cfg.Logger.Level != "info" {
					t.Errorf("LoadConfig() Logger.Level = %v, want info", cfg.Logger.Level)
				}
				if cfg.DatabaseURL != tt.envVars["INVENTORY_DATABASE_URL"] {
					t.Errorf("LoadConfig() DatabaseURL = %v, want %v", cfg.DatabaseURL, tt.envVars["INVENTORY_DATABASE_URL"])
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
					DBName:   "inventory",
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
			errMsg:  "INVENTORY_SERVER_PORT (or INVENTORY_SERVICE_PORT) environment variable is not set",
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
		"INVENTORY_SERVER_PORT",
		"INVENTORY_SERVICE_PORT",
		"INVENTORY_DATABASE_URL",
		"REDIS_URL",
		"KAFKA_BROKERS",
		"INVENTORY_KAFKA_GROUP_ID",
		"JAEGER_ENDPOINT",
	}
	for _, env := range envVars {
		os.Unsetenv(env)
	}
}
