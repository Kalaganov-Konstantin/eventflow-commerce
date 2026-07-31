package database

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoadPostgresConfig_Defaults(t *testing.T) {
	cfg, err := LoadPostgresConfig()
	if err != nil {
		t.Fatalf("LoadPostgresConfig() error = %v", err)
	}

	if cfg.URL != "postgres://postgres:postgres@localhost:5432/eventflow?sslmode=disable" {
		t.Errorf("URL = %q, want the default connection string", cfg.URL)
	}
	if cfg.MaxOpenConns != 25 {
		t.Errorf("MaxOpenConns = %d, want 25", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 5 {
		t.Errorf("MaxIdleConns = %d, want 5", cfg.MaxIdleConns)
	}
	if cfg.MaxLifetime != 5*time.Minute {
		t.Errorf("MaxLifetime = %v, want 5m", cfg.MaxLifetime)
	}
}

func TestLoadPostgresConfig_EnvOverrides(t *testing.T) {
	t.Setenv("DB_URL", "postgres://app:secret@db:5432/orders?sslmode=require")
	t.Setenv("DB_MAX_OPEN_CONNS", "50")
	t.Setenv("DB_MAX_IDLE_CONNS", "10")
	t.Setenv("DB_MAX_LIFETIME", "1h")

	cfg, err := LoadPostgresConfig()
	if err != nil {
		t.Fatalf("LoadPostgresConfig() error = %v", err)
	}

	if cfg.URL != "postgres://app:secret@db:5432/orders?sslmode=require" {
		t.Errorf("URL = %q, want the env value", cfg.URL)
	}
	if cfg.MaxOpenConns != 50 {
		t.Errorf("MaxOpenConns = %d, want 50", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 10 {
		t.Errorf("MaxIdleConns = %d, want 10", cfg.MaxIdleConns)
	}
	if cfg.MaxLifetime != time.Hour {
		t.Errorf("MaxLifetime = %v, want 1h", cfg.MaxLifetime)
	}
}

func TestLoadPostgresConfig_ReturnsErrorForUnparsableEnvValue(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "not-an-int")

	if _, err := LoadPostgresConfig(); err == nil {
		t.Fatal("LoadPostgresConfig() error = nil, want error for a non-numeric DB_MAX_OPEN_CONNS")
	}
}

func TestNewPostgresConnection_ReturnsErrorWhenPingFails(t *testing.T) {
	_, err := NewPostgresConnection(PostgresConfig{
		URL:          "postgres://user:pass@127.0.0.1:1/db?sslmode=disable",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
		MaxLifetime:  time.Minute,
	})
	if err == nil {
		t.Fatal("NewPostgresConnection() error = nil, want error for an unreachable host")
	}
}

func TestDB_Close(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	mock.ExpectClose()

	db := &DB{DB: sqlDB}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
