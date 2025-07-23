package config

import (
	"os"
	"strings"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/config"
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
				"PAYMENT_SERVICE_PORT": "8080",
				"PAYMENT_DATABASE_URL": "postgres://user:pass@localhost:5432/payment?sslmode=disable",
				"REDIS_URL":            "localhost:6379",
				"KAFKA_BROKERS":        "localhost:9092",
				"JAEGER_ENDPOINT":      "http://localhost:14268/api/traces",
			},
			wantErr: false,
		},
		{
			name: "Missing PAYMENT_SERVICE_PORT",
			envVars: map[string]string{
				"PAYMENT_DATABASE_URL": "postgres://user:pass@localhost:5432/payment?sslmode=disable",
				"REDIS_URL":            "localhost:6379",
				"KAFKA_BROKERS":        "localhost:9092",
				"JAEGER_ENDPOINT":      "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "PAYMENT_SERVICE_PORT environment variable is not set",
		},
		{
			name: "Missing PAYMENT_DATABASE_URL",
			envVars: map[string]string{
				"PAYMENT_SERVICE_PORT": "8080",
				"REDIS_URL":            "localhost:6379",
				"KAFKA_BROKERS":        "localhost:9092",
				"JAEGER_ENDPOINT":      "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "PAYMENT_DATABASE_URL environment variable is not set",
		},
		{
			name: "Invalid database URL",
			envVars: map[string]string{
				"PAYMENT_SERVICE_PORT": "8080",
				"PAYMENT_DATABASE_URL": "invalid-url",
				"REDIS_URL":            "localhost:6379",
				"KAFKA_BROKERS":        "localhost:9092",
				"JAEGER_ENDPOINT":      "http://localhost:14268/api/traces",
			},
			wantErr: true,
			errMsg:  "database host is required",
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
					DBName:   "payment",
					SSLMode:  "disable",
				},
				Redis: config.RedisConfig{
					Host: "localhost",
					Port: "6379",
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
					Host: "localhost",
				},
				Kafka: config.KafkaConfig{
					Brokers: []string{"localhost:9092"},
				},
				Jaeger: config.JaegerConfig{
					Endpoint: "http://localhost:14268/api/traces",
				},
			},
			wantErr: true,
			errMsg:  "PAYMENT_SERVICE_PORT environment variable is not set",
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
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func clearEnvVars() {
	envVars := []string{
		"PAYMENT_SERVICE_PORT",
		"PAYMENT_DATABASE_URL",
		"REDIS_URL",
		"KAFKA_BROKERS",
		"JAEGER_ENDPOINT",
	}
	for _, env := range envVars {
		os.Unsetenv(env)
	}
}
