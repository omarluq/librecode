package di

import (
	"context"
	"path/filepath"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/extension"
)

// ExtensionService owns the Lua extension manager.
type ExtensionService struct {
	Manager *extension.Manager
	State   extension.ManagerState
}

// NewExtensionService loads configured Lua extensions.
func NewExtensionService(injector do.Injector) (*ExtensionService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()
	logger := do.MustInvoke[*LoggerService](injector).SlogLogger
	manager := extension.NewManager(logger)
	state := extension.ManagerState{Configured: []extension.ResolvedSource{}, Loaded: []extension.LoadedExtension{}}

	if cfg.Extensions.Enabled {
		resolvedSources, err := resolveExtensionSources(cfg.Extensions.Use)
		if err != nil {
			return nil, oops.In("extension").Code("extension_sources").Wrapf(err, "resolve extension sources")
		}
		state.Configured = resolvedSources

		paths := extensionLoadPaths(resolvedSources)
		if err := manager.LoadPaths(context.Background(), paths); err != nil {
			return nil, oops.In("extension").Code("load_extensions").Wrapf(err, "load lua extensions")
		}
		state.Loaded = manager.Extensions()
	}

	return &ExtensionService{Manager: manager, State: state}, nil
}

// Shutdown closes loaded Lua runtimes.
func (service *ExtensionService) Shutdown() {
	service.Manager.Shutdown()
}

func resolveExtensionSources(configuredSources []extension.ConfiguredSource) ([]extension.ResolvedSource, error) {
	home, err := core.LibrecodeHome()
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(home, extension.LockFileName)
	lockFile, err := extension.ReadLockFile(lockPath)
	if err != nil {
		return nil, err
	}
	installRoot := filepath.Join(home, "extensions", "store")

	return extension.ResolveConfiguredSources(configuredSources, lockFile, installRoot)
}

func extensionLoadPaths(resolvedSources []extension.ResolvedSource) []string {
	paths := make([]string, 0, len(resolvedSources))
	for index := range resolvedSources {
		if resolvedSources[index].LoadPath == "" {
			continue
		}
		paths = append(paths, resolvedSources[index].LoadPath)
	}

	return paths
}
