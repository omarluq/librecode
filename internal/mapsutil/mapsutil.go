// Package mapsutil provides small map-copy helpers used across internal packages.
package mapsutil

import "maps"

// CloneOrEmpty returns a copy of values, or an initialized empty map for nil input.
func CloneOrEmpty[M ~map[K]V, K comparable, V any](values M) M {
	if values == nil {
		return M{}
	}

	return maps.Clone(values)
}

// ClonePreserveNil returns a copy of values, or nil for nil input.
func ClonePreserveNil[M ~map[K]V, K comparable, V any](values M) M {
	if values == nil {
		return nil
	}

	return maps.Clone(values)
}

// CloneOrNil returns a copy of values, or nil for nil or empty input.
func CloneOrNil[M ~map[K]V, K comparable, V any](values M) M {
	if len(values) == 0 {
		return nil
	}

	return maps.Clone(values)
}

// IntMapToAnyMap copies integer map values into a JSON-friendly any map.
func IntMapToAnyMap(values map[string]int) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}
