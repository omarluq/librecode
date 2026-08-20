// Package mapsutil provides small map-copy helpers used across internal packages.
package mapsutil

import "maps"

// CloneOrEmpty returns a copy of string values, or an initialized empty map for nil input.
//
// Use when the result must always be a non-nil map, for example when the copy
// is serialized as a JSON object (an empty map renders as `{}` while nil
// renders as `null`) or when callers may write into it later.
func CloneOrEmpty[V any](values map[string]V) map[string]V {
	if values == nil {
		return map[string]V{}
	}

	return maps.Clone(values)
}

// ClonePreserveNil returns a copy of string values, or nil for nil input.
//
// Use when the copy must distinguish "unset" (nil) from "set but empty", for
// example when copying optional request metadata whose absence carries
// meaning or when merging only if the source was configured.
func ClonePreserveNil[V any](values map[string]V) map[string]V {
	if values == nil {
		return nil
	}

	return maps.Clone(values)
}

// CloneOrNil returns a copy of string values, or nil for nil or empty input.
//
// Use when nil and empty mean the same thing and nil is preferred, for
// example before storing the copy in a struct field with `omitempty` JSON
// tags so empty inputs stay out of serialized output.
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
