package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/viper"
)

type PostgresConfig struct {
	URL          string        `mapstructure:"DB_URL"`
	MaxOpenConns int           `mapstructure:"DB_MAX_OPEN_CONNS"`
	MaxIdleConns int           `mapstructure:"DB_MAX_IDLE_CONNS"`
	MaxLifetime  time.Duration `mapstructure:"DB_MAX_LIFETIME"`
}

type DB struct {
	*sql.DB
}

func NewPostgresConnection(config PostgresConfig) (*DB, error) {
	db, err := sql.Open("postgres", config.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.MaxLifetime)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{DB: db}, nil
}

func (db *DB) Close() error {
	return db.DB.Close()
}

func LoadPostgresConfig() (PostgresConfig, error) {
	v := viper.New()
	v.AutomaticEnv()

	v.SetDefault("DB_URL", "postgres://postgres:postgres@localhost:5432/eventflow?sslmode=disable")
	v.SetDefault("DB_MAX_OPEN_CONNS", 25)
	v.SetDefault("DB_MAX_IDLE_CONNS", 5)
	v.SetDefault("DB_MAX_LIFETIME", "5m")

	var config PostgresConfig
	if err := v.Unmarshal(&config); err != nil {
		return PostgresConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return config, nil
}
