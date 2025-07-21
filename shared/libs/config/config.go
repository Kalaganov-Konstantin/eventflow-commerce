package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// CfgLoader provides a flexible way to load configurations
type CfgLoader struct {
	v *viper.Viper
}

// New creates a new CfgLoader instance
func New(serviceName string) *CfgLoader {
	v := viper.New()

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./configs")

	v.AutomaticEnv()
	v.SetEnvPrefix(strings.ToUpper(serviceName))
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	return &CfgLoader{v: v}
}

// Load loads configuration into the provided struct
func (cl *CfgLoader) Load(cfg interface{}) error {
	if err := cl.v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	if err := cl.v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}

	return nil
}

// SetDefault sets a default value for a config key
func (cl *CfgLoader) SetDefault(key string, value interface{}) {
	cl.v.SetDefault(key, value)
}

// BindEnv binds a specific environment variable to a config key
func (cl *CfgLoader) BindEnv(key string, envVar string) error {
	return cl.v.BindEnv(key, envVar)
}

// GetString returns the value associated with the key as a string.
func (cl *CfgLoader) GetString(key string) string {
	return cl.v.GetString(key)
}
