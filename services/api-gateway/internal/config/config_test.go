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
					Host: "redis",
					Port: "6379",
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
			name:        "URL without scheme",
			serviceURL:  "service:8080",
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
		"API_GATEWAY_PORT":               "8080",
		"REDIS_URL":                      "redis:6379",
		"KAFKA_BROKERS":                  "kafka:9092",
		"JAEGER_ENDPOINT":                "jaeger:14268",
	}

	// Set environment variables
	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set env var %s: %v", key, err)
		}
		defer os.Unsetenv(key) // Clean up after test
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
		os.Unsetenv(envVar)
	}

	// Clean up after test
	defer func() {
		for envVar, originalValue := range originalValues {
			if originalValue != "" {
				os.Setenv(envVar, originalValue)
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
		defer os.Unsetenv(key) // Clean up after test
	}

	_, err := LoadConfig()
	if err == nil {
		t.Error("Expected LoadConfig to fail with invalid configuration values")
	}
}
