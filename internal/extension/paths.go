package extension

import "strings"

const officialExtensionsDir = "extensions"

// DefaultLoadPaths returns official extension roots followed by configured user roots.
func DefaultLoadPaths(configuredPaths []string) []string {
	paths := make([]string, 0, len(configuredPaths)+1)
	seen := map[string]struct{}{}
	for _, path := range append([]string{officialExtensionsDir}, configuredPaths...) {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		paths = append(paths, trimmed)
	}

	return paths
}
