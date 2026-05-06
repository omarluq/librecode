package di

import (
	"context"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/extension"
)

// ExtensionService owns the Lua extension manager.
type ExtensionService struct {
	Manager *extension.Manager
}

// NewExtensionService loads configured Lua extensions.
func NewExtensionService(injector do.Injector) (*ExtensionService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()
	logger := do.MustInvoke[*LoggerService](injector).SlogLogger
	manager := extension.NewManager(logger)

	if cfg.Extensions.Enabled {
		if err := manager.LoadPaths(context.Background(), cfg.Extensions.Paths); err != nil {
			return nil, oops.In("extension").Code("load_extensions").Wrapf(err, "load lua extensions")
		}
	}

	return &ExtensionService{Manager: manager}, nil
}

// Shutdown closes loaded Lua runtimes.
func (service *ExtensionService) Shutdown() {
	service.Manager.Shutdown()
}
