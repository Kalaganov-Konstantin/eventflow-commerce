package config

import (
	"os"
	"testing"

	sharedConfig "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
)

func TestValidate(t *testing.T) {
	testCases := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name: "Valid config",
			config: Config{
				Server: sharedConfig.ServerConfig{
					Host: "localhost",
					Port: "8080",
				},
				Redis: sharedConfig.RedisConfig{
					URL: "redis://redis:6379",
				},
				Kafka: sharedConfig.KafkaConfig{
					Brokers: []string{"kafka:9092"},
				},
				Jaeger: sharedConfig.JaegerConfig{
					Endpoint: "jaeger:14268",
				},
				Database: sharedConfig.DatabaseConfig{
					Host:     "postgres",
					Port:     "5432",
					User:     "test",
					Password: "test",
					DBName:   "test",
					SSLMode:  "disable",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    60,
				},
			},
			expectError: false,
		},
		{
			name: "JWT secret too short",
			config: Config{
				JWTSecret:              "short",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    60,
				},
			},
			expectError: true,
		},
		{
			name: "Invalid order service URL",
			config: Config{
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "invalid-url-without-scheme",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    60,
				},
			},
			expectError: true,
		},
		{
			name: "Empty order service URL",
			config: Config{
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    60,
				},
			},
			expectError: true,
		},
		{
			name: "Invalid rate limit config - negative requests",
			config: Config{
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: -1,
					WindowDuration:    60,
				},
			},
			expectError: true,
		},
		{
			name: "Invalid rate limit config - zero window",
			config: Config{
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    0,
				},
			},
			expectError: true,
		},
		{
			name: "URL without host",
			config: Config{
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    60,
				},
			},
			expectError: true,
		},
		{
			name: "Malformed URL",
			config: Config{
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://[invalid-ipv6:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: 100,
					WindowDuration:    60,
				},
			},
			expectError: true,
		},
		// The cases above all fail before the server port check, so none of them reach the
		// later validations. The cases below set every earlier field so validation runs deep
		// enough to exercise the specific check under test.
		{
			name: "Redis URL missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: ""},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Kafka brokers missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: nil},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Jaeger endpoint missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: ""},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Order service URL missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Payment service URL missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Inventory service URL missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Notification service URL missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "JWT secret invalid propagates through full validation",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "short",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Order service URL fails format validation with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "not-a-valid-url",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Payment service URL fails format validation with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "not-a-valid-url",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Inventory service URL fails format validation with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "not-a-valid-url",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Notification service URL fails format validation with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "not-a-valid-url",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Rate limit invalid propagates through full validation",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: -1, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Circuit breaker invalid propagates through full validation",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
				CircuitBreaker:         CircuitBreakerConfig{FailureThreshold: -1},
			},
			expectError: true,
		},
		{
			name: "Database host missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "", Port: "5432", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Database port missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "", User: "test", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Database user missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "", Password: "test", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Database password missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "", DBName: "test",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
		{
			name: "Database name missing with full config",
			config: Config{
				Server: sharedConfig.ServerConfig{Port: "8080"},
				Redis:  sharedConfig.RedisConfig{URL: "redis://redis:6379"},
				Kafka:  sharedConfig.KafkaConfig{Brokers: []string{"kafka:9092"}},
				Jaeger: sharedConfig.JaegerConfig{Endpoint: "jaeger:14268"},
				Database: sharedConfig.DatabaseConfig{
					Host: "postgres", Port: "5432", User: "test", Password: "test", DBName: "",
				},
				JWTSecret:              "this-is-a-very-long-secret-key-for-jwt-validation",
				OrderServiceURL:        "http://order:8080",
				PaymentServiceURL:      "http://payment:8080",
				InventoryServiceURL:    "http://inventory:8080",
				NotificationServiceURL: "http://notification:8080",
				RateLimit:              RateLimitConfig{RequestsPerMinute: 100, WindowDuration: 60},
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()

			if tc.expectError && err == nil {
				t.Error("Expected validation error, but got none")
			}

			if !tc.expectError && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestRateLimitConfigValidate(t *testing.T) {
	testCases := []struct {
		name        string
		config      RateLimitConfig
		expectError bool
	}{
		{
			name: "Valid rate limit config",
			config: RateLimitConfig{
				RequestsPerMinute: 100,
				WindowDuration:    60,
			},
			expectError: false,
		},
		{
			name: "Zero requests per minute",
			config: RateLimitConfig{
				RequestsPerMinute: 0,
				WindowDuration:    60,
			},
			expectError: true,
		},
		{
			name: "Negative requests per minute",
			config: RateLimitConfig{
				RequestsPerMinute: -1,
				WindowDuration:    60,
			},
			expectError: true,
		},
		{
			name: "Zero window duration",
			config: RateLimitConfig{
				RequestsPerMinute: 100,
				WindowDuration:    0,
			},
			expectError: true,
		},
		{
			name: "Negative window duration",
			config: RateLimitConfig{
				RequestsPerMinute: 100,
				WindowDuration:    -1,
			},
			expectError: true,
		},
		{
			name: "Requests per minute above maximum",
			config: RateLimitConfig{
				RequestsPerMinute: 10001,
				WindowDuration:    60,
			},
			expectError: true,
		},
		{
			name: "Window duration above maximum",
			config: RateLimitConfig{
				RequestsPerMinute: 100,
				WindowDuration:    3601,
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()

			if tc.expectError && err == nil {
				t.Error("Expected validation error, but got none")
			}

			if !tc.expectError && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestCircuitBreakerConfigValidate(t *testing.T) {
	testCases := []struct {
		name        string
		config      CircuitBreakerConfig
		expectError bool
	}{
		{
			name:        "Zero value falls back to defaults, not an error",
			config:      CircuitBreakerConfig{},
			expectError: false,
		},
		{
			name: "Valid explicit config",
			config: CircuitBreakerConfig{
				FailureThreshold:   5,
				WindowSeconds:      60,
				OpenTimeoutSeconds: 30,
			},
			expectError: false,
		},
		{
			name:        "Negative failure threshold",
			config:      CircuitBreakerConfig{FailureThreshold: -1},
			expectError: true,
		},
		{
			name:        "Negative window",
			config:      CircuitBreakerConfig{WindowSeconds: -1},
			expectError: true,
		},
		{
			name:        "Negative open timeout",
			config:      CircuitBreakerConfig{OpenTimeoutSeconds: -1},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()

			if tc.expectError && err == nil {
				t.Error("Expected validation error, but got none")
			}

			if !tc.expectError && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestValidateServiceURL(t *testing.T) {
	cfg := &Config{}

	testCases := []struct {
		name        string
		serviceURL  string
		envName     string
		expectError bool
	}{
		{
			name:        "Valid HTTP URL",
			serviceURL:  "http://service:8080",
			envName:     "SERVICE_URL",
			expectError: false,
		},
		{
			name:        "Valid HTTPS URL",
			serviceURL:  "https://service.example.com",
			envName:     "SERVICE_URL",
			expectError: false,
		},
		{
			name:        "Empty URL",
			serviceURL:  "",
			envName:     "SERVICE_URL",
			expectError: true,
		},
		{
			// "service:8080" actually parses with Scheme="service" (opaque form), which
			// exercises the host check below rather than the scheme check; a protocol-relative
			// URL is what genuinely produces an empty Scheme with a non-empty Host.
			name:        "URL without scheme",
			serviceURL:  "//service:8080",
			envName:     "SERVICE_URL",
			expectError: true,
		},
		{
			name:        "URL without host",
			serviceURL:  "http://",
			envName:     "SERVICE_URL",
			expectError: true,
		},
		{
			name:        "Malformed URL",
			serviceURL:  "http://[invalid-ipv6:8080",
			envName:     "SERVICE_URL",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := cfg.validateServiceURL(tc.serviceURL, tc.envName)

			if tc.expectError && err == nil {
				t.Error("Expected validation error, but got none")
			}

			if !tc.expectError && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestValidateJWTSecret(t *testing.T) {
	testCases := []struct {
		name        string
		secret      string
		expectError bool
	}{
		{
			name:        "Valid secret",
			secret:      "this-is-a-very-long-secret-key-for-jwt-validation",
			expectError: false,
		},
		{
			name:        "Empty secret",
			secret:      "",
			expectError: true,
		},
		{
			name:        "Too short",
			secret:      "short",
			expectError: true,
		},
		{
			// Long enough to pass the length check, so it reaches the weak-value blocklist.
			name:        "Weak value from blocklist",
			secret:      "CHANGE_ME_IN_PRODUCTION_GENERATE_WITH_openssl_rand_base64_32",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{JWTSecret: tc.secret}
			err := cfg.validateJWTSecret()

			if tc.expectError && err == nil {
				t.Error("Expected validation error, but got none")
			}

			if !tc.expectError && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Set required environment variables
	envVars := map[string]string{
		"JWT_SECRET":                     "this-is-a-very-long-secret-key-for-jwt-validation",
		"ORDER_SERVICE_URL":              "http://order:8080",
		"PAYMENT_SERVICE_URL":            "http://payment:8080",
		"INVENTORY_SERVICE_URL":          "http://inventory:8080",
		"NOTIFICATION_SERVICE_URL":       "http://notification:8080",
		"RATE_LIMIT_REQUESTS_PER_MINUTE": "100",
		"RATE_LIMIT_WINDOW_DURATION":     "60",
		"API_GATEWAY_DATABASE_URL":       "postgres://test:test@postgres:5432/test?sslmode=disable",
		"API_GATEWAY_SERVER_PORT":        "8080",
		"REDIS_URL":                      "redis:6379",
		"KAFKA_BROKERS":                  "kafka:9092",
		"OTEL_EXPORTER_OTLP_ENDPOINT":    "jaeger:14268",
	}

	// Set environment variables
	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set env var %s: %v", key, err)
		}
		defer func() { _ = os.Unsetenv(key) }() // Clean up after test
	}

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.JWTSecret != envVars["JWT_SECRET"] {
		t.Errorf("Expected JWT secret %s, got %s", envVars["JWT_SECRET"], config.JWTSecret)
	}

	if config.OrderServiceURL != envVars["ORDER_SERVICE_URL"] {
		t.Errorf("Expected order service URL %s, got %s", envVars["ORDER_SERVICE_URL"], config.OrderServiceURL)
	}

	if config.RateLimit.RequestsPerMinute != 100 {
		t.Errorf("Expected rate limit requests per minute 100, got %d", config.RateLimit.RequestsPerMinute)
	}

	if config.RateLimit.WindowDuration != 60 {
		t.Errorf("Expected rate limit window duration 60, got %d", config.RateLimit.WindowDuration)
	}

	if config.Server.Port != "8080" {
		t.Errorf("Expected server port 8080, got %s", config.Server.Port)
	}
}

func TestLoadConfig_PortFallsBackToLegacyVariable(t *testing.T) {
	envVars := map[string]string{
		"JWT_SECRET":                  "this-is-a-very-long-secret-key-for-jwt-validation",
		"ORDER_SERVICE_URL":           "http://order:8080",
		"PAYMENT_SERVICE_URL":         "http://payment:8080",
		"INVENTORY_SERVICE_URL":       "http://inventory:8080",
		"NOTIFICATION_SERVICE_URL":    "http://notification:8080",
		"API_GATEWAY_DATABASE_URL":    "postgres://test:test@postgres:5432/test?sslmode=disable",
		"API_GATEWAY_PORT":            "9090",
		"REDIS_URL":                   "redis:6379",
		"KAFKA_BROKERS":               "kafka:9092",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "jaeger:14268",
	}

	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set env var %s: %v", key, err)
		}
		defer func() { _ = os.Unsetenv(key) }()
	}

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.Server.Port != "9090" {
		t.Errorf("Expected server port 9090 from legacy API_GATEWAY_PORT, got %s", config.Server.Port)
	}
}

// The performance profile raises the limit through the environment, so this variable name is part
// of the contract with docker-compose and the Makefile.
func TestLoadConfig_RateLimitFromEnv(t *testing.T) {
	envVars := map[string]string{
		"JWT_SECRET":                                 "this-is-a-very-long-secret-key-for-jwt-validation",
		"ORDER_SERVICE_URL":                          "http://order:8080",
		"PAYMENT_SERVICE_URL":                        "http://payment:8080",
		"INVENTORY_SERVICE_URL":                      "http://inventory:8080",
		"NOTIFICATION_SERVICE_URL":                   "http://notification:8080",
		"API_GATEWAY_DATABASE_URL":                   "postgres://test:test@postgres:5432/test?sslmode=disable",
		"API_GATEWAY_SERVER_PORT":                    "8080",
		"REDIS_URL":                                  "redis:6379",
		"KAFKA_BROKERS":                              "kafka:9092",
		"OTEL_EXPORTER_OTLP_ENDPOINT":                "jaeger:14268",
		"API_GATEWAY_RATE_LIMIT_REQUESTS_PER_MINUTE": "10000",
	}

	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set env var %s: %v", key, err)
		}
		defer func() { _ = os.Unsetenv(key) }()
	}

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.RateLimit.RequestsPerMinute != 10000 {
		t.Errorf("Expected rate limit requests per minute 10000 from environment, got %d", config.RateLimit.RequestsPerMinute)
	}
}

func TestLoadConfig_MissingRequiredEnvVars(t *testing.T) {
	// Unset any existing environment variables that might interfere
	requiredEnvVars := []string{
		"JWT_SECRET",
		"ORDER_SERVICE_URL",
		"PAYMENT_SERVICE_URL",
		"INVENTORY_SERVICE_URL",
		"NOTIFICATION_SERVICE_URL",
	}

	// Store original values and unset them
	originalValues := make(map[string]string)
	for _, envVar := range requiredEnvVars {
		originalValues[envVar] = os.Getenv(envVar)
		if err := os.Unsetenv(envVar); err != nil {
			t.Fatalf("Failed to unset env var %s: %v", envVar, err)
		}
	}

	// Clean up after test
	defer func() {
		for envVar, originalValue := range originalValues {
			if originalValue != "" {
				_ = os.Setenv(envVar, originalValue)
			}
		}
	}()

	_, err := LoadConfig()
	if err == nil {
		t.Error("Expected LoadConfig to fail with missing required environment variables")
	}
}

func TestLoadConfig_InvalidEnvValues(t *testing.T) {
	// Set invalid environment variables
	envVars := map[string]string{
		"JWT_SECRET":                     "short", // Too short
		"ORDER_SERVICE_URL":              "invalid-url",
		"PAYMENT_SERVICE_URL":            "http://payment:8080",
		"INVENTORY_SERVICE_URL":          "http://inventory:8080",
		"NOTIFICATION_SERVICE_URL":       "http://notification:8080",
		"RATE_LIMIT_REQUESTS_PER_MINUTE": "-1", // Invalid
		"RATE_LIMIT_WINDOW_DURATION":     "60",
	}

	// Set environment variables
	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set env var %s: %v", key, err)
		}
		defer func() { _ = os.Unsetenv(key) }() // Clean up after test
	}

	_, err := LoadConfig()
	if err == nil {
		t.Error("Expected LoadConfig to fail with invalid configuration values")
	}
}

func TestLoadConfig_UnparsableDatabaseURL(t *testing.T) {
	envVars := map[string]string{
		"JWT_SECRET":                  "this-is-a-very-long-secret-key-for-jwt-validation",
		"ORDER_SERVICE_URL":           "http://order:8080",
		"PAYMENT_SERVICE_URL":         "http://payment:8080",
		"INVENTORY_SERVICE_URL":       "http://inventory:8080",
		"NOTIFICATION_SERVICE_URL":    "http://notification:8080",
		"API_GATEWAY_SERVER_PORT":     "8080",
		"REDIS_URL":                   "redis:6379",
		"KAFKA_BROKERS":               "kafka:9092",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "jaeger:14268",
		// The invalid percent-escape makes url.Parse itself fail, rather than just
		// producing an empty or invalid field.
		"API_GATEWAY_DATABASE_URL": "postgres://%zz@postgres:5432/test",
	}

	for key, value := range envVars {
		t.Setenv(key, value)
	}

	if _, err := LoadConfig(); err == nil {
		t.Error("Expected LoadConfig to fail with an unparsable database URL")
	}
}

func TestLoadConfig_WeakJWTSecretFailsValidation(t *testing.T) {
	envVars := map[string]string{
		// Long enough to pass the length check, so LoadConfig's call to cfg.Validate()
		// fails on the weak-value blocklist instead, deeper in validation.
		"JWT_SECRET":                  "CHANGE_ME_IN_PRODUCTION_GENERATE_WITH_openssl_rand_base64_32",
		"ORDER_SERVICE_URL":           "http://order:8080",
		"PAYMENT_SERVICE_URL":         "http://payment:8080",
		"INVENTORY_SERVICE_URL":       "http://inventory:8080",
		"NOTIFICATION_SERVICE_URL":    "http://notification:8080",
		"API_GATEWAY_SERVER_PORT":     "8080",
		"REDIS_URL":                   "redis:6379",
		"KAFKA_BROKERS":               "kafka:9092",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "jaeger:14268",
		"API_GATEWAY_DATABASE_URL":    "postgres://test:test@postgres:5432/test?sslmode=disable",
	}

	for key, value := range envVars {
		t.Setenv(key, value)
	}

	if _, err := LoadConfig(); err == nil {
		t.Error("Expected LoadConfig to fail validation with a weak JWT secret")
	}
}
