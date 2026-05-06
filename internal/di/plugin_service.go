package di

import (
	"context"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/plugin"
)

// PluginService owns the Lua plugin manager.
type PluginService struct {
	Manager *plugin.Manager
}

// NewPluginService loads configured Lua plugins.
func NewPluginService(injector do.Injector) (*PluginService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()
	logger := do.MustInvoke[*LoggerService](injector).SlogLogger
	manager := plugin.NewManager(logger)

	if cfg.Plugins.Enabled {
		if err := manager.LoadPaths(context.Background(), cfg.Plugins.Paths); err != nil {
			return nil, oops.In("plugin").Code("load_plugins").Wrapf(err, "load lua plugins")
		}
	}

	return &PluginService{Manager: manager}, nil
}

// Shutdown closes loaded Lua runtimes.
func (service *PluginService) Shutdown() {
	service.Manager.Shutdown()
}
