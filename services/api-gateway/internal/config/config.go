package config

import (
	"fmt"
	"net/url"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/config"
)

type Config struct {
	Server                 config.ServerConfig   `mapstructure:"server"`
	Database               config.DatabaseConfig `mapstructure:"database"`
	Redis                  config.RedisConfig    `mapstructure:"redis"`
	Kafka                  config.KafkaConfig    `mapstructure:"kafka"`
	Jaeger                 config.JaegerConfig   `mapstructure:"jaeger"`
	OrderServiceURL        string                `mapstructure:"order_service_url"`
	PaymentServiceURL      string                `mapstructure:"payment_service_url"`
	InventoryServiceURL    string                `mapstructure:"inventory_service_url"`
	NotificationServiceURL string                `mapstructure:"notification_service_url"`
}

func LoadConfig() (*Config, error) {
	var cfg Config

	loader := config.New("api_gateway")
	loader.SetDefault("server.host", "0.0.0.0")

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
