package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/samber/mo"
	"github.com/spf13/viper"

	"github.com/omarluq/librecode/internal/core"
)

// Load resolves configuration from defaults, environment variables, and an optional file.
func Load(path string) mo.Result[*Config] {
	loaded, err := LoadResolved(path)
	if err != nil {
		return mo.Err[*Config](err)
	}

	return mo.Ok(loaded.Config)
}

// LoadedConfig contains the resolved config and the file path it came from, if any.
type LoadedConfig struct {
	Config *Config
	Path   string
}

// LoadResolved resolves configuration and reports the file path used, if any.
func LoadResolved(path string) (LoadedConfig, error) {
	viperInstance := viper.New()
	setDefaults(viperInstance)

	viperInstance.SetEnvPrefix("LIBRECODE")
	viperInstance.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viperInstance.AutomaticEnv()

	if path != "" {
		viperInstance.SetConfigFile(path)
	} else {
		viperInstance.SetConfigName("config")
		viperInstance.SetConfigType("yaml")

		for _, configPath := range defaultConfigPaths() {
			viperInstance.AddConfigPath(configPath)
		}
	}

	if err := viperInstance.ReadInConfig(); err != nil {
		var notFoundErr viper.ConfigFileNotFoundError
		if !errors.As(err, &notFoundErr) || path != "" {
			return LoadedConfig{}, fmt.Errorf("config: read: %w", err)
		}
	}

	var cfg Config
	if err := unmarshalConfig(viperInstance, &cfg); err != nil {
		return LoadedConfig{}, err
	}

	if err := cfg.Validate(); err != nil {
		return LoadedConfig{}, err
	}

	return LoadedConfig{Config: &cfg, Path: viperInstance.ConfigFileUsed()}, nil
}

func defaultConfigPaths() []string {
	paths := []string{filepath.Join(".", core.ConfigDirName)}
	if home, err := core.LibrecodeHome(); err == nil {
		paths = append(paths, home)
	}

	return paths
}

func setDefaults(viperInstance *viper.Viper) {
	viperInstance.SetDefault("app.name", "librecode")
	viperInstance.SetDefault("app.env", "development")
	viperInstance.SetDefault("app.working_loader.text", "Shenaniganing...")
	viperInstance.SetDefault("logging.level", "info")
	viperInstance.SetDefault("logging.format", "pretty")
	viperInstance.SetDefault("database.path", "")
	viperInstance.SetDefault("database.apply_migrations", true)
	viperInstance.SetDefault("database.max_open_conns", 1)
	viperInstance.SetDefault("database.max_idle_conns", 1)
	viperInstance.SetDefault("database.conn_max_lifetime", defaultDatabaseConnMaxLifetime)
	viperInstance.SetDefault("database.busy_timeout", defaultDatabaseBusyTimeout)
	viperInstance.SetDefault("extensions.enabled", true)
	viperInstance.SetDefault("extensions.use", []string{defaultLocalExtensionSource})
	viperInstance.SetDefault("assistant.provider", "openai-codex")
	viperInstance.SetDefault("assistant.model", "gpt-5.5")
	viperInstance.SetDefault("assistant.thinking_level", "off")
	viperInstance.SetDefault("assistant.retry.enabled", true)
	viperInstance.SetDefault("assistant.retry.max_attempts", defaultRetryMaxAttempts)
	viperInstance.SetDefault("assistant.retry.base_delay", defaultRetryBaseDelay)
	viperInstance.SetDefault("assistant.retry.max_delay", defaultRetryMaxDelay)
	viperInstance.SetDefault("context.preflight_enabled", true)
	viperInstance.SetDefault("context.output_reserve_tokens", 0)
	viperInstance.SetDefault("context.provider_reserve_tokens", defaultProviderReserveTokens)
	viperInstance.SetDefault("context.safety_margin_tokens", defaultSafetyMarginTokens)
	viperInstance.SetDefault("models.discovery.enabled", true)
	viperInstance.SetDefault("models.discovery.source_url", "https://models.dev/api.json")
	viperInstance.SetDefault("models.discovery.cache_ttl", defaultDiscoveryCacheTTL)
	viperInstance.SetDefault("models.discovery.fetch_timeout", defaultDiscoveryFetchTimeout)
	viperInstance.SetDefault("cache.enabled", true)
	viperInstance.SetDefault("cache.capacity", defaultCacheCapacity)
	viperInstance.SetDefault("cache.ttl", defaultCacheTTL)
}
