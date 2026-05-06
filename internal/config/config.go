// Package config loads and validates application configuration.
package config

import (
	"fmt"
	"time"
)

// Config is the fully resolved application configuration.
type Config struct {
	App      AppConfig      `json:"app" mapstructure:"app" yaml:"app"`
	Logging  LoggingConfig  `json:"logging" mapstructure:"logging" yaml:"logging"`
	Database DatabaseConfig `json:"database" mapstructure:"database" yaml:"database"`
	Plugins  PluginsConfig  `json:"plugins" mapstructure:"plugins" yaml:"plugins"`
	Agent    AgentConfig    `json:"agent" mapstructure:"agent" yaml:"agent"`
	Cache    CacheConfig    `json:"cache" mapstructure:"cache" yaml:"cache"`
	KSQL     KSQLConfig     `json:"ksql" mapstructure:"ksql" yaml:"ksql"`
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

// DatabaseConfig contains SQLite persistence settings.
type DatabaseConfig struct {
	Path            string        `json:"path" mapstructure:"path" yaml:"path"`
	ApplyMigrations bool          `json:"apply_migrations" mapstructure:"apply_migrations" yaml:"apply_migrations"`
	MaxOpenConns    int           `json:"max_open_conns" mapstructure:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns" mapstructure:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime" mapstructure:"conn_max_lifetime" yaml:"conn_max_lifetime"`
}

// PluginsConfig controls Lua plugin discovery and execution.
type PluginsConfig struct {
	Enabled bool     `json:"enabled" mapstructure:"enabled" yaml:"enabled"`
	Paths   []string `json:"paths" mapstructure:"paths" yaml:"paths"`
}

// AgentConfig controls the local agent runtime defaults.
type AgentConfig struct {
	Provider      string `json:"provider" mapstructure:"provider" yaml:"provider"`
	Model         string `json:"model" mapstructure:"model" yaml:"model"`
	ThinkingLevel string `json:"thinking_level" mapstructure:"thinking_level" yaml:"thinking_level"`
}

// CacheConfig controls samber/hot response caching.
type CacheConfig struct {
	Enabled  bool          `json:"enabled" mapstructure:"enabled" yaml:"enabled"`
	Capacity int           `json:"capacity" mapstructure:"capacity" yaml:"capacity"`
	TTL      time.Duration `json:"ttl" mapstructure:"ttl" yaml:"ttl"`
}

// KSQLConfig controls optional ksqlDB REST integration.
type KSQLConfig struct {
	Endpoint string        `json:"endpoint" mapstructure:"endpoint" yaml:"endpoint"`
	Timeout  time.Duration `json:"timeout" mapstructure:"timeout" yaml:"timeout"`
}

// Validate ensures the configuration is internally consistent.
func (config *Config) Validate() error {
	if config.App.Name == "" {
		return fmt.Errorf("config: app.name is required")
	}

	switch config.App.Env {
	case "development", "test", "production":
	default:
		return fmt.Errorf("config: app.env must be development, test, or production")
	}

	switch config.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: logging.level must be debug, info, warn, or error")
	}

	switch config.Logging.Format {
	case "pretty", "json":
	default:
		return fmt.Errorf("config: logging.format must be pretty or json")
	}

	if config.Database.MaxOpenConns < 1 {
		return fmt.Errorf("config: database.max_open_conns must be greater than zero")
	}
	if config.Database.MaxIdleConns < 0 {
		return fmt.Errorf("config: database.max_idle_conns cannot be negative")
	}
	if config.Database.ConnMaxLifetime < 0 {
		return fmt.Errorf("config: database.conn_max_lifetime cannot be negative")
	}
	if config.Cache.Capacity < 1 {
		return fmt.Errorf("config: cache.capacity must be greater than zero")
	}
	if config.Cache.TTL <= 0 {
		return fmt.Errorf("config: cache.ttl must be greater than zero")
	}
	if config.KSQL.Timeout <= 0 {
		return fmt.Errorf("config: ksql.timeout must be greater than zero")
	}

	return nil
}

// IsDev reports whether the application is running in development mode.
func (config *Config) IsDev() bool {
	return config.App.Env == "development"
}
