package config

// Common configuration types that can be composed by services

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port string `mapstructure:"port"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	SSLMode  string `mapstructure:"sslmode"`
}

type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	URL      string `mapstructure:"url"`
}

type KafkaConfig struct {
	Brokers  []string `mapstructure:"brokers"`
	GroupID  string   `mapstructure:"group_id"`
	DLQTopic string   `mapstructure:"dlq_topic"`
}

type JaegerConfig struct {
	Endpoint string `mapstructure:"endpoint"`
}

type ServiceConfig struct {
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`
}

type LoggerConfig struct {
	Level       string   `mapstructure:"level"`
	Environment string   `mapstructure:"environment"`
	OutputPaths []string `mapstructure:"output_paths"`
}
