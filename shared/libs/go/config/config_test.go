package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew_EnvPrefixAndKeyReplacer(t *testing.T) {
	t.Setenv("ORDER_SERVER_PORT", "9090")

	loader := New("order")

	if got := loader.GetString("server.port"); got != "9090" {
		t.Errorf("GetString(%q) = %q, want %q", "server.port", got, "9090")
	}
}

func TestLoad_MissingConfigFileIsNotError(t *testing.T) {
	loader := New("missing_config_service")

	var cfg struct{}
	if err := loader.Load(&cfg); err != nil {
		t.Fatalf("Load() error = %v, want nil when config file is absent", err)
	}
}

func TestLoad_UnmarshalsDefaults(t *testing.T) {
	loader := New("load_defaults_service")
	loader.SetDefault("server.port", "8080")

	var cfg struct {
		Server struct {
			Port string `mapstructure:"port"`
		} `mapstructure:"server"`
	}

	if err := loader.Load(&cfg); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != "8080" {
		t.Errorf("cfg.Server.Port = %q, want %q", cfg.Server.Port, "8080")
	}
}

func TestLoad_ReturnsErrorForMalformedConfigFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("not: [valid: yaml"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	loader := New("malformed_config_service")
	var cfg struct{}
	if err := loader.Load(&cfg); err == nil {
		t.Fatal("Load() error = nil, want error for a malformed config file")
	}
}

func TestLoad_ReturnsErrorWhenValueDoesNotMatchTargetType(t *testing.T) {
	loader := New("bad_type_service")
	loader.SetDefault("server.port", "not-a-number")

	var cfg struct {
		Server struct {
			Port int `mapstructure:"port"`
		} `mapstructure:"server"`
	}

	if err := loader.Load(&cfg); err == nil {
		t.Fatal("Load() error = nil, want error when the value cannot decode into the target type")
	}
}

func TestSetDefault(t *testing.T) {
	loader := New("default_only_service")
	loader.SetDefault("logger.level", "info")

	if got := loader.GetString("logger.level"); got != "info" {
		t.Errorf("GetString() = %q, want %q", got, "info")
	}
}

func TestBindEnv(t *testing.T) {
	t.Setenv("CUSTOM_PORT_VAR", "1234")

	loader := New("bind_env_service")
	if err := loader.BindEnv("server.port", "CUSTOM_PORT_VAR"); err != nil {
		t.Fatalf("BindEnv() error = %v", err)
	}

	if got := loader.GetString("server.port"); got != "1234" {
		t.Errorf("GetString() = %q, want %q", got, "1234")
	}
}

func TestBindEnv_MultipleVariables_FirstNonEmptyWins(t *testing.T) {
	t.Setenv("PRIMARY_PORT_VAR", "9090")
	t.Setenv("FALLBACK_PORT_VAR", "1234")

	loader := New("multi_bind_service")
	if err := loader.BindEnv("server.port", "PRIMARY_PORT_VAR", "FALLBACK_PORT_VAR"); err != nil {
		t.Fatalf("BindEnv() error = %v", err)
	}

	if got := loader.GetString("server.port"); got != "9090" {
		t.Errorf("GetString() = %q, want %q (first variable must win)", got, "9090")
	}
}

func TestEnv_OverridesDefault(t *testing.T) {
	t.Setenv("OVERRIDE_SERVICE_SERVER_PORT", "9999")

	loader := New("override_service")
	loader.SetDefault("server.port", "8080")

	if got := loader.GetString("server.port"); got != "9999" {
		t.Errorf("GetString() = %q, want %q (env must override default)", got, "9999")
	}
}
