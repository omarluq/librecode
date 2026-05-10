// Package config loads and validates application configuration.
package config

import (
	"fmt"
	"time"
)

// Config is the fully resolved application configuration.
type Config struct {
	Assistant  AssistantConfig  `json:"assistant" mapstructure:"assistant" yaml:"assistant"`
	App        AppConfig        `json:"app" mapstructure:"app" yaml:"app"`
	Logging    LoggingConfig    `json:"logging" mapstructure:"logging" yaml:"logging"`
	Extensions ExtensionsConfig `json:"extensions" mapstructure:"extensions" yaml:"extensions"`
	KSQL       KSQLConfig       `json:"ksql" mapstructure:"ksql" yaml:"ksql"`
	Database   DatabaseConfig   `json:"database" mapstructure:"database" yaml:"database"`
	Cache      CacheConfig      `json:"cache" mapstructure:"cache" yaml:"cache"`
}

// AppConfig contains application identity and environment settings.
type AppConfig struct {
	Name          string   `json:"name" mapstructure:"name" yaml:"name"`
	Env           string   `json:"env" mapstructure:"env" yaml:"env"`
	WorkingLoader LoaderUI `json:"working_loader" mapstructure:"working_loader" yaml:"working_loader"`
}

// LoaderUI controls text shown while an assistant response is in progress.
type LoaderUI struct {
	Text string `json:"text" mapstructure:"text" yaml:"text"`
}

// LoggingConfig contains runtime logging settings.
type LoggingConfig struct {
	Level  string `json:"level" mapstructure:"level" yaml:"level"`
	Format string `json:"format" mapstructure:"format" yaml:"format"`
}

// DatabaseConfig contains session database persistence settings.
type DatabaseConfig struct {
	Path            string        `json:"path" mapstructure:"path" yaml:"path"`
	ApplyMigrations bool          `json:"apply_migrations" mapstructure:"apply_migrations" yaml:"apply_migrations"`
	MaxOpenConns    int           `json:"max_open_conns" mapstructure:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns" mapstructure:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime" mapstructure:"conn_max_lifetime" yaml:"conn_max_lifetime"`
}

// ExtensionsConfig controls workflow extension discovery and execution.
type ExtensionsConfig struct {
	Paths   []string `json:"paths" mapstructure:"paths" yaml:"paths"`
	Enabled bool     `json:"enabled" mapstructure:"enabled" yaml:"enabled"`
}

// AssistantConfig controls the assistant runtime defaults.
type AssistantConfig struct {
	Provider      string      `json:"provider" mapstructure:"provider" yaml:"provider"`
	Model         string      `json:"model" mapstructure:"model" yaml:"model"`
	ThinkingLevel string      `json:"thinking_level" mapstructure:"thinking_level" yaml:"thinking_level"`
	Retry         RetryConfig `json:"retry" mapstructure:"retry" yaml:"retry"`
}

// RetryConfig controls transient model request retries.
type RetryConfig struct {
	BaseDelay   time.Duration `json:"base_delay" mapstructure:"base_delay" yaml:"base_delay"`
	MaxDelay    time.Duration `json:"max_delay" mapstructure:"max_delay" yaml:"max_delay"`
	MaxAttempts int           `json:"max_attempts" mapstructure:"max_attempts" yaml:"max_attempts"`
	Enabled     bool          `json:"enabled" mapstructure:"enabled" yaml:"enabled"`
}

// Normalized returns retry settings with safe defaults for omitted values.
func (retry RetryConfig) Normalized() RetryConfig {
	if retry.MaxAttempts <= 0 {
		retry.MaxAttempts = 3
	}
	if retry.BaseDelay <= 0 {
		retry.BaseDelay = 2 * time.Second
	}
	if retry.MaxDelay <= 0 {
		retry.MaxDelay = 30 * time.Second
	}
	if retry.MaxDelay < retry.BaseDelay {
		retry.MaxDelay = retry.BaseDelay
	}

	return retry
}

// CacheConfig controls assistant response caching.
type CacheConfig struct {
	Enabled  bool          `json:"enabled" mapstructure:"enabled" yaml:"enabled"`
	Capacity int           `json:"capacity" mapstructure:"capacity" yaml:"capacity"`
	TTL      time.Duration `json:"ttl" mapstructure:"ttl" yaml:"ttl"`
}

// KSQLConfig controls optional ksqlDB integration.
type KSQLConfig struct {
	Endpoint string        `json:"endpoint" mapstructure:"endpoint" yaml:"endpoint"`
	Timeout  time.Duration `json:"timeout" mapstructure:"timeout" yaml:"timeout"`
}

// Validate ensures the configuration is internally consistent.
func (config *Config) Validate() error {
	validators := []func() error{
		config.validateApp,
		config.validateLogging,
		config.validateDatabase,
		config.validateAssistant,
		config.validateCache,
		config.validateKSQL,
	}

	for _, validate := range validators {
		if err := validate(); err != nil {
			return err
		}
	}

	return nil
}

// IsDev reports whether the application is running in development mode.
func (config *Config) IsDev() bool {
	return config.App.Env == "development"
}

func (config *Config) validateApp() error {
	if config.App.Name == "" {
		return fmt.Errorf("config: app.name is required")
	}
	if config.App.WorkingLoader.Text == "" {
		return fmt.Errorf("config: app.working_loader.text is required")
	}

	switch config.App.Env {
	case "development", "test", "production":
		return nil
	default:
		return fmt.Errorf("config: app.env must be development, test, or production")
	}
}

func (config *Config) validateLogging() error {
	switch config.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: logging.level must be debug, info, warn, or error")
	}

	switch config.Logging.Format {
	case "pretty", "json":
		return nil
	default:
		return fmt.Errorf("config: logging.format must be pretty or json")
	}
}

func (config *Config) validateDatabase() error {
	if config.Database.MaxOpenConns < 1 {
		return fmt.Errorf("config: database.max_open_conns must be greater than zero")
	}
	if config.Database.MaxIdleConns < 0 {
		return fmt.Errorf("config: database.max_idle_conns cannot be negative")
	}
	if config.Database.ConnMaxLifetime < 0 {
		return fmt.Errorf("config: database.conn_max_lifetime cannot be negative")
	}

	return nil
}

func (config *Config) validateAssistant() error {
	retry := config.Assistant.Retry.Normalized()
	if config.Assistant.Retry.MaxAttempts < 0 {
		return fmt.Errorf("config: assistant.retry.max_attempts cannot be negative")
	}
	if config.Assistant.Retry.BaseDelay < 0 {
		return fmt.Errorf("config: assistant.retry.base_delay cannot be negative")
	}
	if config.Assistant.Retry.MaxDelay < 0 {
		return fmt.Errorf("config: assistant.retry.max_delay cannot be negative")
	}
	config.Assistant.Retry = retry

	return nil
}

func (config *Config) validateCache() error {
	if config.Cache.Capacity < 1 {
		return fmt.Errorf("config: cache.capacity must be greater than zero")
	}
	if config.Cache.TTL <= 0 {
		return fmt.Errorf("config: cache.ttl must be greater than zero")
	}

	return nil
}

func (config *Config) validateKSQL() error {
	if config.KSQL.Timeout <= 0 {
		return fmt.Errorf("config: ksql.timeout must be greater than zero")
	}

	return nil
}
