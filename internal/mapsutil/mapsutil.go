// Package mapsutil provides small map-copy helpers used across internal packages.
package mapsutil

import "maps"

// CloneOrEmpty returns a copy of string values, or an initialized empty map for nil input.
func CloneOrEmpty[V any](values map[string]V) map[string]V {
	if values == nil {
		return map[string]V{}
	}

	return maps.Clone(values)
}

// ClonePreserveNil returns a copy of string values, or nil for nil input.
func ClonePreserveNil[V any](values map[string]V) map[string]V {
	if values == nil {
		return nil
	}

	return maps.Clone(values)
}

// CloneOrNil returns a copy of string values, or nil for nil or empty input.
func CloneOrNil[V any](values map[string]V) map[string]V {
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
