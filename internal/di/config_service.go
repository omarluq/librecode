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
	Interactive       bool
}

// ConfigService provides access to the resolved application configuration.
type ConfigService struct {
	cfg         *config.Config
	path        string
	interactive bool
}

// NewConfigService loads configuration from the injector's configured path.
func NewConfigService(injector do.Injector) (*ConfigService, error) {
	path, err := do.InvokeNamed[string](injector, ConfigPathKey)
	if err != nil {
		return nil, err
	}

	loaded, err := config.LoadResolved(path)
	if err != nil {
		return nil, serviceError(err, "load config")
	}

	cfg := loaded.Config

	overrides, err := do.InvokeNamed[ConfigOverrides](injector, ConfigOverridesKey)
	if err != nil {
		return nil, err
	}

	if overrides.DisableExtensions {
		cfg.Extensions.Enabled = false
	}

	return &ConfigService{cfg: cfg, path: loaded.Path, interactive: overrides.Interactive}, nil
}

// Get returns the resolved application configuration.
func (s *ConfigService) Get() *config.Config {
	return s.cfg
}

// Path returns the config file path used to load configuration, if any.
func (s *ConfigService) Path() string {
	return s.path
}

// Interactive reports whether the process is running the terminal UI.
func (s *ConfigService) Interactive() bool {
	return s.interactive
}
