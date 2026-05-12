package extension

import "strings"

// LocalLoadPaths returns explicitly configured path: extension sources with duplicates removed.
func LocalLoadPaths(configuredSources []ConfiguredSource) ([]string, error) {
	paths := []string{}
	for _, configuredSource := range configuredSources {
		ref, err := ParseSourceRef(configuredSource.Source, configuredSource.Version)
		if err != nil {
			return nil, err
		}
		if path, ok := ref.LocalPath(); ok {
			paths = append(paths, path)
		}
	}

	return dedupePaths(paths), nil
}

func dedupePaths(configuredPaths []string) []string {
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
