// Package config loads and validates application configuration.
package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/omarluq/librecode/internal/extension"
)

// Config is the fully resolved application configuration.
type Config struct {
	App        AppConfig        `json:"app" mapstructure:"app" yaml:"app"`
	Logging    LoggingConfig    `json:"logging" mapstructure:"logging" yaml:"logging"`
	Extensions ExtensionsConfig `json:"extensions" mapstructure:"extensions" yaml:"extensions"`
	Models     ModelsConfig     `json:"models" mapstructure:"models" yaml:"models"`
	Assistant  AssistantConfig  `json:"assistant" mapstructure:"assistant" yaml:"assistant"`
	Database   DatabaseConfig   `json:"database" mapstructure:"database" yaml:"database"`
	Context    ContextConfig    `json:"context" mapstructure:"context" yaml:"context"`
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
	BusyTimeout     time.Duration `json:"busy_timeout" mapstructure:"busy_timeout" yaml:"busy_timeout"`
}

// ExtensionsConfig controls workflow extension discovery and execution.
type ExtensionsConfig struct {
	Use     []ExtensionUse `json:"use" mapstructure:"use" yaml:"use"`
	Enabled bool           `json:"enabled" mapstructure:"enabled" yaml:"enabled"`
}

// ExtensionUse declares one extension source.
type ExtensionUse = extension.ConfiguredSource

// AssistantConfig controls the assistant runtime defaults.
type AssistantConfig struct {
	Provider      string      `json:"provider" mapstructure:"provider" yaml:"provider"`
	Model         string      `json:"model" mapstructure:"model" yaml:"model"`
	ThinkingLevel string      `json:"thinking_level" mapstructure:"thinking_level" yaml:"thinking_level"`
	Retry         RetryConfig `json:"retry" mapstructure:"retry" yaml:"retry"`
}

// ContextConfig controls local context-window budgeting before provider requests.
type ContextConfig struct {
	OutputReserveTokens   int  `mapstructure:"output_reserve_tokens"`
	ProviderReserveTokens int  `mapstructure:"provider_reserve_tokens"`
	SafetyMarginTokens    int  `mapstructure:"safety_margin_tokens"`
	PreflightEnabled      bool `mapstructure:"preflight_enabled"`
}

// ModelsConfig controls model catalog discovery.
type ModelsConfig struct {
	Discovery ModelDiscoveryConfig `json:"discovery" mapstructure:"discovery" yaml:"discovery"`
}

// ModelDiscoveryConfig controls remote model catalog discovery.
type ModelDiscoveryConfig struct {
	SourceURL    string        `json:"source_url" mapstructure:"source_url" yaml:"source_url"`
	CacheTTL     time.Duration `json:"cache_ttl" mapstructure:"cache_ttl" yaml:"cache_ttl"`
	FetchTimeout time.Duration `json:"fetch_timeout" mapstructure:"fetch_timeout" yaml:"fetch_timeout"`
	Enabled      bool          `json:"enabled" mapstructure:"enabled" yaml:"enabled"`
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
		retry.MaxAttempts = defaultRetryMaxAttempts
	}

	if retry.BaseDelay <= 0 {
		retry.BaseDelay = defaultRetryBaseDelay
	}

	if retry.MaxDelay <= 0 {
		retry.MaxDelay = defaultRetryMaxDelay
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

const (
	envDevelopment = "development"
	envTest        = "test"
	envProduction  = "production"
)

// Validate ensures the configuration is internally consistent.
func (config *Config) Validate() error {
	validators := []func() error{
		config.validateApp,
		config.validateLogging,
		config.validateDatabase,
		config.validateExtensions,
		config.validateAssistant,
		config.validateContext,
		config.validateModels,
		config.validateCache,
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
	return config.App.Env == envDevelopment
}

func (config *Config) validateApp() error {
	if config.App.Name == "" {
		return errors.New("config: app.name is required")
	}

	if config.App.WorkingLoader.Text == "" {
		return errors.New("config: app.working_loader.text is required")
	}

	switch config.App.Env {
	case envDevelopment, envTest, envProduction:
		return nil
	default:
		return errors.New("config: app.env must be development, test, or production")
	}
}

func (config *Config) validateLogging() error {
	switch config.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("config: logging.level must be debug, info, warn, or error")
	}

	switch config.Logging.Format {
	case "pretty", "json":
		return nil
	default:
		return errors.New("config: logging.format must be pretty or json")
	}
}

func (config *Config) validateDatabase() error {
	if config.Database.MaxOpenConns < 1 {
		return errors.New("config: database.max_open_conns must be greater than zero")
	}

	if config.Database.MaxIdleConns < 0 {
		return errors.New("config: database.max_idle_conns cannot be negative")
	}

	if config.Database.ConnMaxLifetime < 0 {
		return errors.New("config: database.conn_max_lifetime cannot be negative")
	}

	if config.Database.BusyTimeout < 0 {
		return errors.New("config: database.busy_timeout cannot be negative")
	}

	return nil
}

func (config *Config) validateExtensions() error {
	for _, extensionUse := range config.Extensions.Use {
		if extensionUse.Source == "" {
			return errors.New("config: extensions.use source is required")
		}

		if _, err := extension.ParseSourceRef(extensionUse.Source, extensionUse.Version); err != nil {
			return fmt.Errorf("config: invalid extensions.use source %q: %w", extensionUse.Source, err)
		}
	}

	return nil
}

func (config *Config) validateAssistant() error {
	retry := config.Assistant.Retry.Normalized()
	if config.Assistant.Retry.MaxAttempts < 0 {
		return errors.New("config: assistant.retry.max_attempts cannot be negative")
	}

	if config.Assistant.Retry.BaseDelay < 0 {
		return errors.New("config: assistant.retry.base_delay cannot be negative")
	}

	if config.Assistant.Retry.MaxDelay < 0 {
		return errors.New("config: assistant.retry.max_delay cannot be negative")
	}

	config.Assistant.Retry = retry

	return nil
}

func (config *Config) validateContext() error {
	if config.Context.OutputReserveTokens < 0 {
		return errors.New("config: context.output_reserve_tokens cannot be negative")
	}

	if config.Context.ProviderReserveTokens < 0 {
		return errors.New("config: context.provider_reserve_tokens cannot be negative")
	}

	if config.Context.SafetyMarginTokens < 0 {
		return errors.New("config: context.safety_margin_tokens cannot be negative")
	}

	return nil
}

func (config *Config) validateModels() error {
	if config.Models.Discovery.Enabled && config.Models.Discovery.SourceURL == "" {
		return errors.New("config: models.discovery.source_url is required when discovery is enabled")
	}

	if config.Models.Discovery.CacheTTL < 0 {
		return errors.New("config: models.discovery.cache_ttl cannot be negative")
	}

	if config.Models.Discovery.FetchTimeout < 0 {
		return errors.New("config: models.discovery.fetch_timeout cannot be negative")
	}

	return nil
}

func (config *Config) validateCache() error {
	if config.Cache.Capacity < 1 {
		return errors.New("config: cache.capacity must be greater than zero")
	}

	if config.Cache.TTL <= 0 {
		return errors.New("config: cache.ttl must be greater than zero")
	}

	return nil
}
