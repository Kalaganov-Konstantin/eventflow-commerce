package config

import (
	"fmt"
	"net/url"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
)

type Config struct {
	Server                 config.ServerConfig   `mapstructure:"server"`
	Database               config.DatabaseConfig `mapstructure:"database"`
	Redis                  config.RedisConfig    `mapstructure:"redis"`
	Kafka                  config.KafkaConfig    `mapstructure:"kafka"`
	Jaeger                 config.JaegerConfig   `mapstructure:"jaeger"`
	Logger                 config.LoggerConfig   `mapstructure:"logger"`
	Service                config.ServiceConfig  `mapstructure:"service"`
	OrderServiceURL        string                `mapstructure:"order_service_url"`
	PaymentServiceURL      string                `mapstructure:"payment_service_url"`
	InventoryServiceURL    string                `mapstructure:"inventory_service_url"`
	NotificationServiceURL string                `mapstructure:"notification_service_url"`
	JWTSecret              string                `mapstructure:"jwt_secret"`
	RateLimit              RateLimitConfig       `mapstructure:"rate_limit"`
	ProxyTimeout           int                   `mapstructure:"proxy_timeout_seconds"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `mapstructure:"requests_per_minute"`
	WindowDuration    int `mapstructure:"window_duration_seconds"`
}

func (r RateLimitConfig) Validate() error {
	if r.RequestsPerMinute <= 0 {
		return fmt.Errorf("rate limit requests per minute must be positive, got %d", r.RequestsPerMinute)
	}
	if r.RequestsPerMinute > 10000 {
		return fmt.Errorf("rate limit requests per minute too high, maximum is 10000, got %d", r.RequestsPerMinute)
	}
	if r.WindowDuration <= 0 {
		return fmt.Errorf("rate limit window duration must be positive, got %d", r.WindowDuration)
	}
	if r.WindowDuration > 3600 {
		return fmt.Errorf("rate limit window duration too long, maximum is 3600 seconds, got %d", r.WindowDuration)
	}
	return nil
}

func LoadConfig() (*Config, error) {
	var cfg Config

	loader := config.New("api_gateway")
	loader.SetDefault("server.host", "0.0.0.0")
	loader.SetDefault("rate_limit.requests_per_minute", 100)
	loader.SetDefault("rate_limit.window_duration_seconds", 60)
	loader.SetDefault("proxy_timeout_seconds", 30)
	loader.SetDefault("logger.level", "info")
	loader.SetDefault("logger.environment", "development")
	loader.SetDefault("logger.output_paths", []string{"stdout"})
	loader.SetDefault("service.name", "api-gateway")
	loader.SetDefault("service.version", "1.0.0")

	// Explicitly bind environment variables
	if err := loader.BindEnv("server.port", "API_GATEWAY_PORT"); err != nil {
		return nil, fmt.Errorf("failed to bind server.port: %w", err)
	}
	if err := loader.BindEnv("database.url", "API_GATEWAY_DATABASE_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind database.url: %w", err)
	}
	if err := loader.BindEnv("redis.host", "REDIS_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind redis.host: %w", err)
	}
	if err := loader.BindEnv("kafka.brokers", "KAFKA_BROKERS"); err != nil {
		return nil, fmt.Errorf("failed to bind kafka.brokers: %w", err)
	}
	if err := loader.BindEnv("jaeger.endpoint", "JAEGER_ENDPOINT"); err != nil {
		return nil, fmt.Errorf("failed to bind jaeger.endpoint: %w", err)
	}
	if err := loader.BindEnv("order_service_url", "ORDER_SERVICE_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind order_service_url: %w", err)
	}
	if err := loader.BindEnv("payment_service_url", "PAYMENT_SERVICE_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind payment_service_url: %w", err)
	}
	if err := loader.BindEnv("inventory_service_url", "INVENTORY_SERVICE_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind inventory_service_url: %w", err)
	}
	if err := loader.BindEnv("notification_service_url", "NOTIFICATION_SERVICE_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind notification_service_url: %w", err)
	}
	if err := loader.BindEnv("jwt_secret", "JWT_SECRET"); err != nil {
		return nil, fmt.Errorf("failed to bind jwt_secret: %w", err)
	}

	err := loader.Load(&cfg)
	if err != nil {
		return nil, err
	}

	// Get the database URL string directly from viper
	dbURLString := loader.GetString("database.url")
	if dbURLString == "" {
		return nil, fmt.Errorf("API_GATEWAY_DATABASE_URL environment variable is not set")
	}

	parsedURL, err := url.Parse(dbURLString)
	if err != nil {
		return nil, fmt.Errorf("invalid API_GATEWAY_DATABASE_URL: %w", err)
	}

	// Populate DatabaseConfig fields from parsed URL
	cfg.Database.Host = parsedURL.Hostname()
	cfg.Database.Port = parsedURL.Port()
	cfg.Database.User = parsedURL.User.Username()
	cfg.Database.Password, _ = parsedURL.User.Password()
	cfg.Database.DBName = parsedURL.Path[1:] // Remove leading slash
	cfg.Database.SSLMode = parsedURL.Query().Get("sslmode")

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	// Validation for Server
	if c.Server.Port == "" {
		return fmt.Errorf("API_GATEWAY_PORT environment variable is not set")
	}

	// Validation for Redis
	if c.Redis.Host == "" {
		return fmt.Errorf("REDIS_URL environment variable is not set")
	}

	// Validation for Kafka
	if len(c.Kafka.Brokers) == 0 {
		return fmt.Errorf("KAFKA_BROKERS environment variable is not set")
	}

	// Validation for Jaeger
	if c.Jaeger.Endpoint == "" {
		return fmt.Errorf("JAEGER_ENDPOINT environment variable is not set")
	}

	// Validation for Service URLs
	if c.OrderServiceURL == "" {
		return fmt.Errorf("ORDER_SERVICE_URL environment variable is not set")
	}
	if c.PaymentServiceURL == "" {
		return fmt.Errorf("PAYMENT_SERVICE_URL environment variable is not set")
	}
	if c.InventoryServiceURL == "" {
		return fmt.Errorf("INVENTORY_SERVICE_URL environment variable is not set")
	}
	if c.NotificationServiceURL == "" {
		return fmt.Errorf("NOTIFICATION_SERVICE_URL environment variable is not set")
	}

	// Validation for JWT
	if err := c.validateJWTSecret(); err != nil {
		return err
	}

	// Validate URL formats
	if err := c.validateServiceURL(c.OrderServiceURL, "ORDER_SERVICE_URL"); err != nil {
		return err
	}
	if err := c.validateServiceURL(c.PaymentServiceURL, "PAYMENT_SERVICE_URL"); err != nil {
		return err
	}
	if err := c.validateServiceURL(c.InventoryServiceURL, "INVENTORY_SERVICE_URL"); err != nil {
		return err
	}
	if err := c.validateServiceURL(c.NotificationServiceURL, "NOTIFICATION_SERVICE_URL"); err != nil {
		return err
	}

	// Validate rate limiting configuration
	if err := c.RateLimit.Validate(); err != nil {
		return fmt.Errorf("rate limit configuration invalid: %w", err)
	}

	// Final validation of database fields after parsing (which happens in LoadConfig)
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required in API_GATEWAY_DATABASE_URL")
	}
	if c.Database.Port == "" {
		return fmt.Errorf("database port is required in API_GATEWAY_DATABASE_URL")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database user is required in API_GATEWAY_DATABASE_URL")
	}
	if c.Database.Password == "" {
		return fmt.Errorf("database password is required in API_GATEWAY_DATABASE_URL")
	}
	if c.Database.DBName == "" {
		return fmt.Errorf("database name is required in API_GATEWAY_DATABASE_URL")
	}

	return nil
}

func (c *Config) validateJWTSecret() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET environment variable is not set")
	}

	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 characters long for security, got %d", len(c.JWTSecret))
	}

	// Check for common weak secrets
	weakSecrets := []string{
		"CHANGE_ME_IN_PRODUCTION_GENERATE_WITH_openssl_rand_base64_32",
		"your-jwt-secret-key-here",
		"aVeryLongAndSecureSecretKeyForDevelopment32+Chars",
		"secret",
		"password",
		"123456",
		"test",
		"development",
	}

	for _, weak := range weakSecrets {
		if c.JWTSecret == weak {
			return fmt.Errorf("JWT_SECRET appears to be a default/weak value. Please generate a strong secret using: openssl rand -base64 32")
		}
	}

	return nil
}

func (c *Config) validateServiceURL(serviceURL, envName string) error {
	if serviceURL == "" {
		return fmt.Errorf("%s environment variable is not set", envName)
	}

	parsedURL, err := url.Parse(serviceURL)
	if err != nil {
		return fmt.Errorf("invalid %s URL format: %w", envName, err)
	}

	if parsedURL.Scheme == "" {
		return fmt.Errorf("%s must include a scheme (http:// or https://)", envName)
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("%s must include a valid host", envName)
	}

	return nil
}
