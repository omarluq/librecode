package config

import (
	"errors"
	"fmt"
	"strings"

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
}
