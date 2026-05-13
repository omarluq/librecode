package di

import "github.com/omarluq/librecode/internal/extension"

// ExtensionLoadPathsForTest exposes extension load-path deduplication to external tests.
func ExtensionLoadPathsForTest(resolvedSources []extension.ResolvedSource) []string {
	return extensionLoadPaths(resolvedSources)
}

// ExtensionLockPathForTest exposes extension lockfile resolution to external tests.
func ExtensionLockPathForTest(configPath, home string) string {
	return extensionLockPath(configPath, home)
}
