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
	configService := do.MustInvoke[*ConfigService](injector)
	cfg := configService.Get()
	logger := do.MustInvoke[*LoggerService](injector).SlogLogger
	manager := extension.NewManager(logger)
	state := extension.ManagerState{Configured: []extension.ResolvedSource{}, Loaded: []extension.LoadedExtension{}}

	if cfg.Extensions.Enabled {
		resolvedSources, err := resolveExtensionSources(cfg.Extensions.Use, configService.Path())
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

func resolveExtensionSources(
	configuredSources []extension.ConfiguredSource,
	configPath string,
) ([]extension.ResolvedSource, error) {
	home, err := core.LibrecodeHome()
	if err != nil {
		return nil, err
	}
	lockPath := extensionLockPath(configPath, home)
	lockFile, err := extension.ReadLockFile(lockPath)
	if err != nil {
		return nil, err
	}
	installRoot := filepath.Join(home, "extensions", "store")

	return extension.ResolveConfiguredSources(configuredSources, lockFile, installRoot)
}

func extensionLockPath(configPath, home string) string {
	if configPath != "" && filepath.Base(filepath.Dir(configPath)) == core.ConfigDirName {
		return filepath.Join(filepath.Dir(configPath), extension.LockFileName)
	}

	return filepath.Join(home, extension.LockFileName)
}

func extensionLoadPaths(resolvedSources []extension.ResolvedSource) []string {
	paths := make([]string, 0, len(resolvedSources))
	seen := map[string]struct{}{}
	for index := range resolvedSources {
		path := resolvedSources[index].LoadPath
		if path == "" {
			continue
		}
		key := extensionLoadPathKey(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, path)
	}

	return paths
}

func extensionLoadPathKey(path string) string {
	cleanPath := filepath.Clean(path)
	absolutePath, err := filepath.Abs(cleanPath)
	if err != nil {
		return cleanPath
	}
	if realPath, err := filepath.EvalSymlinks(absolutePath); err == nil {
		return realPath
	}

	return absolutePath
}
