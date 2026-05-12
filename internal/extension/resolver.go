package extension

import (
	"fmt"
	"path/filepath"
)

const officialVimModeVersion = "v0.1.0"

var officialExtensions = map[string]LockedExtension{
	"vim-mode": {
		Resolved: "github:omarluq/librecode//extensions/vim-mode",
		Version:  officialVimModeVersion,
	},
}

// ResolvedSource describes a configured extension after applying aliases and locks.
type ResolvedSource struct {
	Configured ConfiguredSource
	Ref        SourceRef
	Lock       LockedExtension
	LoadPath   string
	Name       string
	Status     string
}

// ResolveConfiguredSources resolves configured extension entries using a lockfile.
func ResolveConfiguredSources(
	configuredSources []ConfiguredSource,
	lockFile LockFile,
	installRoot string,
) ([]ResolvedSource, error) {
	resolved := make([]ResolvedSource, 0, len(configuredSources))
	for _, configuredSource := range configuredSources {
		ref, err := ParseSourceRef(configuredSource.Source, configuredSource.Version)
		if err != nil {
			return nil, err
		}

		resolvedSource, err := resolveSource(configuredSource, ref, lockFile, installRoot)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, resolvedSource)
	}

	return resolved, nil
}

func resolveSource(
	configuredSource ConfiguredSource,
	ref SourceRef,
	lockFile LockFile,
	installRoot string,
) (ResolvedSource, error) {
	locked := lockFile.Extensions[ref.Key()]
	if configuredSource.Version != "" {
		locked.Version = configuredSource.Version
	}

	resolvedSource := ResolvedSource{
		Configured: configuredSource,
		Ref:        ref,
		Lock:       locked,
		LoadPath:   "",
		Name:       ref.Value,
		Status:     "configured",
	}

	switch ref.Scheme {
	case sourceSchemePath:
		resolvedSource.LoadPath = ref.Value
		resolvedSource.Status = "path"
		return resolvedSource, nil
	case sourceSchemeOfficial:
		return resolveOfficialSource(&resolvedSource, installRoot)
	case sourceSchemeGitHub:
		return resolveGitHubSource(&resolvedSource, installRoot), nil
	default:
		return ResolvedSource{}, fmt.Errorf("extension: unsupported source scheme %q", ref.Scheme)
	}
}

func resolveOfficialSource(resolvedSource *ResolvedSource, installRoot string) (ResolvedSource, error) {
	official, ok := officialExtensions[resolvedSource.Ref.Value]
	if !ok {
		return ResolvedSource{}, fmt.Errorf("extension: unknown official extension %q", resolvedSource.Ref.Value)
	}
	if resolvedSource.Lock.Resolved == "" {
		resolvedSource.Lock.Resolved = official.Resolved
	}
	if resolvedSource.Lock.Version == "" {
		resolvedSource.Lock.Version = official.Version
	}
	resolvedSource.LoadPath = filepath.Join(installRoot, "official", resolvedSource.Ref.Value)
	resolvedSource.Status = "missing"
	resolvedSource.Name = resolvedSource.Ref.Value

	return *resolvedSource, nil
}

func resolveGitHubSource(resolvedSource *ResolvedSource, installRoot string) ResolvedSource {
	resolvedSource.LoadPath = filepath.Join(installRoot, "github", filepath.FromSlash(resolvedSource.Ref.Value))
	resolvedSource.Status = "missing"
	resolvedSource.Name = resolvedSource.Ref.Value

	return *resolvedSource
}
