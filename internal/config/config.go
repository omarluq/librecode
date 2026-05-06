// Package config loads and validates application configuration.
package config

import "fmt"

// Config is the fully resolved application configuration.
type Config struct {
	App     AppConfig     `json:"app" mapstructure:"app" yaml:"app"`
	Logging LoggingConfig `json:"logging" mapstructure:"logging" yaml:"logging"`
}

// AppConfig contains application identity and environment settings.
type AppConfig struct {
	Name string `json:"name" mapstructure:"name" yaml:"name"`
	Env  string `json:"env" mapstructure:"env" yaml:"env"`
}

// LoggingConfig contains runtime logging settings.
type LoggingConfig struct {
	Level  string `json:"level" mapstructure:"level" yaml:"level"`
	Format string `json:"format" mapstructure:"format" yaml:"format"`
}

// Validate ensures the configuration is internally consistent.
func (c *Config) Validate() error {
	if c.App.Name == "" {
		return fmt.Errorf("config: app.name is required")
	}

	switch c.App.Env {
	case "development", "test", "production":
	default:
		return fmt.Errorf("config: app.env must be development, test, or production")
	}

	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: logging.level must be debug, info, warn, or error")
	}

	switch c.Logging.Format {
	case "pretty", "json":
	default:
		return fmt.Errorf("config: logging.format must be pretty or json")
	}

	return nil
}

// IsDev reports whether the application is running in development mode.
func (c *Config) IsDev() bool {
	return c.App.Env == "development"
}
