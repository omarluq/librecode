package di

import "github.com/omarluq/librecode/internal/extension"

// ExtensionLoadPathsForTest exposes extension load-path deduplication to external tests.
func ExtensionLoadPathsForTest(resolvedSources []extension.ResolvedSource) []string {
	return extensionLoadPaths(resolvedSources)
}
