package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/config"
)

// ConfigPathKey stores the optional config file path in the injector.
const ConfigPathKey = "config.path"

// ConfigOverridesKey stores process-level config overrides in the injector.
const ConfigOverridesKey = "config.overrides"

// ConfigOverrides contains CLI/runtime overrides applied after config loading.
type ConfigOverrides struct {
	DisableExtensions bool
}

// ConfigService provides access to the resolved application configuration.
type ConfigService struct {
	cfg *config.Config
}

// NewConfigService loads configuration from the injector's configured path.
func NewConfigService(injector do.Injector) (*ConfigService, error) {
	path := do.MustInvokeNamed[string](injector, ConfigPathKey)

	cfg, err := config.Load(path).Get()
	if err != nil {
		return nil, err
	}
	overrides := do.MustInvokeNamed[ConfigOverrides](injector, ConfigOverridesKey)
	if overrides.DisableExtensions {
		cfg.Extensions.Enabled = false
	}

	return &ConfigService{cfg: cfg}, nil
}

// Get returns the resolved application configuration.
func (s *ConfigService) Get() *config.Config {
	return s.cfg
}
