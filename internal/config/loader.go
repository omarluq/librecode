package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/mo"
	"github.com/spf13/viper"
)

// Load resolves configuration from defaults, environment variables, and an optional file.
func Load(path string) mo.Result[*Config] {
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
		viperInstance.AddConfigPath(".")
		viperInstance.AddConfigPath("$HOME/.config/librecode")
	}

	if err := viperInstance.ReadInConfig(); err != nil {
		var notFoundErr viper.ConfigFileNotFoundError
		if !errors.As(err, &notFoundErr) || path != "" {
			return mo.Err[*Config](fmt.Errorf("config: read: %w", err))
		}
	}

	var cfg Config
	if err := viperInstance.Unmarshal(&cfg); err != nil {
		return mo.Err[*Config](fmt.Errorf("config: unmarshal: %w", err))
	}

	if err := cfg.Validate(); err != nil {
		return mo.Err[*Config](err)
	}

	return mo.Ok(&cfg)
}

func setDefaults(viperInstance *viper.Viper) {
	viperInstance.SetDefault("app.name", "librecode")
	viperInstance.SetDefault("app.env", "development")
	viperInstance.SetDefault("logging.level", "info")
	viperInstance.SetDefault("logging.format", "pretty")
	viperInstance.SetDefault("database.path", "")
	viperInstance.SetDefault("database.apply_migrations", true)
	viperInstance.SetDefault("database.max_open_conns", 1)
	viperInstance.SetDefault("database.max_idle_conns", 1)
	viperInstance.SetDefault("database.conn_max_lifetime", 30*time.Minute)
	viperInstance.SetDefault("extensions.enabled", true)
	viperInstance.SetDefault("extensions.paths", []string{".librecode/extensions"})
	viperInstance.SetDefault("assistant.provider", "openai-codex")
	viperInstance.SetDefault("assistant.model", "gpt-5.5")
	viperInstance.SetDefault("assistant.thinking_level", "off")
	viperInstance.SetDefault("assistant.retry.enabled", true)
	viperInstance.SetDefault("assistant.retry.max_attempts", 3)
	viperInstance.SetDefault("assistant.retry.base_delay", 2*time.Second)
	viperInstance.SetDefault("assistant.retry.max_delay", 30*time.Second)
	viperInstance.SetDefault("cache.enabled", true)
	viperInstance.SetDefault("cache.capacity", 512)
	viperInstance.SetDefault("cache.ttl", 10*time.Minute)
	viperInstance.SetDefault("ksql.endpoint", "")
	viperInstance.SetDefault("ksql.timeout", 10*time.Second)
}
