package config

import (
	"fmt"
	"net/url"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/config"
)

// DatabasePoolConfig sizes the postgres connection pool.
type DatabasePoolConfig struct {
	MaxOpenConns int           `mapstructure:"max_open_conns"`
	MaxIdleConns int           `mapstructure:"max_idle_conns"`
	MaxLifetime  time.Duration `mapstructure:"max_lifetime"`
}

// OutboxConfig sizes the outbox relay poll loop.
type OutboxConfig struct {
	RelayInterval  time.Duration `mapstructure:"relay_interval"`
	RelayBatchSize int           `mapstructure:"relay_batch_size"`
}

// InventoryClientConfig controls the HTTP client used to reserve and release stock synchronously.
type InventoryClientConfig struct {
	Timeout time.Duration `mapstructure:"timeout"`
}

// PaymentClientConfig controls the HTTP client used to refund a payment during compensation.
type PaymentClientConfig struct {
	Timeout time.Duration `mapstructure:"timeout"`
}

type Config struct {
	Server       config.ServerConfig   `mapstructure:"server"`
	Database     config.DatabaseConfig `mapstructure:"database"`
	DatabasePool DatabasePoolConfig    `mapstructure:"database_pool"`
	// DatabaseURL is the raw connection string used to open the pool; Database
	// above holds the same information split into fields for validation.
	DatabaseURL         string                `mapstructure:"-"`
	Redis               config.RedisConfig    `mapstructure:"redis"`
	Kafka               config.KafkaConfig    `mapstructure:"kafka"`
	Outbox              OutboxConfig          `mapstructure:"outbox"`
	Jaeger              config.JaegerConfig   `mapstructure:"jaeger"`
	Logger              config.LoggerConfig   `mapstructure:"logger"`
	Service             config.ServiceConfig  `mapstructure:"service"`
	InventoryServiceURL string                `mapstructure:"inventory_service_url"`
	InventoryClient     InventoryClientConfig `mapstructure:"inventory_client"`
	PaymentServiceURL   string                `mapstructure:"payment_service_url"`
	PaymentClient       PaymentClientConfig   `mapstructure:"payment_client"`
}

func LoadConfig() (*Config, error) {
	var cfg Config

	loader := config.New("order")
	loader.SetDefault("server.host", "0.0.0.0")
	loader.SetDefault("kafka.group_id", "order-service")
	loader.SetDefault("kafka.dlq_topic", "orders.events.dlq")
	loader.SetDefault("logger.level", "info")
	loader.SetDefault("logger.environment", "development")
	loader.SetDefault("logger.output_paths", []string{"stdout"})
	loader.SetDefault("service.name", "order")
	loader.SetDefault("service.version", "1.0.0")
	loader.SetDefault("database_pool.max_open_conns", 25)
	loader.SetDefault("database_pool.max_idle_conns", 5)
	loader.SetDefault("database_pool.max_lifetime", "5m")
	loader.SetDefault("outbox.relay_interval", "1s")
	loader.SetDefault("outbox.relay_batch_size", 100)
	loader.SetDefault("inventory_client.timeout", "5s")
	loader.SetDefault("payment_client.timeout", "5s")

	// Explicitly bind environment variables
	if err := loader.BindEnv("server.port", "ORDER_SERVER_PORT", "ORDER_SERVICE_PORT"); err != nil {
		return nil, fmt.Errorf("failed to bind server.port: %w", err)
	}
	if err := loader.BindEnv("database.url", "ORDER_DATABASE_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind database.url: %w", err)
	}
	if err := loader.BindEnv("redis.url", "REDIS_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind redis.url: %w", err)
	}
	if err := loader.BindEnv("kafka.brokers", "KAFKA_BROKERS"); err != nil {
		return nil, fmt.Errorf("failed to bind kafka.brokers: %w", err)
	}
	if err := loader.BindEnv("kafka.group_id", "ORDER_KAFKA_GROUP_ID"); err != nil {
		return nil, fmt.Errorf("failed to bind kafka.group_id: %w", err)
	}
	if err := loader.BindEnv("jaeger.endpoint", "OTEL_EXPORTER_OTLP_ENDPOINT"); err != nil {
		return nil, fmt.Errorf("failed to bind jaeger.endpoint: %w", err)
	}
	if err := loader.BindEnv("inventory_service_url", "INVENTORY_SERVICE_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind inventory_service_url: %w", err)
	}
	if err := loader.BindEnv("payment_service_url", "PAYMENT_SERVICE_URL"); err != nil {
		return nil, fmt.Errorf("failed to bind payment_service_url: %w", err)
	}

	err := loader.Load(&cfg)
	if err != nil {
		return nil, err
	}

	// Get the database URL string directly from viper
	dbURLString := loader.GetString("database.url")
	if dbURLString == "" {
		return nil, fmt.Errorf("ORDER_DATABASE_URL environment variable is not set")
	}
	cfg.DatabaseURL = dbURLString

	parsedURL, err := url.Parse(dbURLString)
	if err != nil {
		return nil, fmt.Errorf("invalid ORDER_DATABASE_URL: %w", err)
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
		return fmt.Errorf("ORDER_SERVER_PORT (or ORDER_SERVICE_PORT) environment variable is not set")
	}

	// Validation for Redis
	if c.Redis.URL == "" {
		return fmt.Errorf("REDIS_URL environment variable is not set")
	}

	// Validation for Kafka
	if len(c.Kafka.Brokers) == 0 {
		return fmt.Errorf("KAFKA_BROKERS environment variable is not set")
	}

	// Validation for Jaeger
	if c.Jaeger.Endpoint == "" {
		return fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT environment variable is not set")
	}

	// Validation for the inventory client
	if c.InventoryServiceURL == "" {
		return fmt.Errorf("INVENTORY_SERVICE_URL environment variable is not set")
	}

	// Validation for the payment client
	if c.PaymentServiceURL == "" {
		return fmt.Errorf("PAYMENT_SERVICE_URL environment variable is not set")
	}

	// Final validation of database fields after parsing (which happens in LoadConfig)
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required in ORDER_DATABASE_URL")
	}
	if c.Database.Port == "" {
		return fmt.Errorf("database port is required in ORDER_DATABASE_URL")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database user is required in ORDER_DATABASE_URL")
	}
	if c.Database.Password == "" {
		return fmt.Errorf("database password is required in ORDER_DATABASE_URL")
	}
	if c.Database.DBName == "" {
		return fmt.Errorf("database name is required in ORDER_DATABASE_URL")
	}

	return nil
}
