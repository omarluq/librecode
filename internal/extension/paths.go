package extension

import "strings"

// DefaultLoadPaths returns configured extension roots with whitespace trimmed and duplicates removed.
func DefaultLoadPaths(configuredPaths []string) []string {
	paths := make([]string, 0, len(configuredPaths))
	seen := map[string]struct{}{}
	for _, path := range configuredPaths {
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
