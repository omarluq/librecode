package provider

import "maps"

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}

	return maps.Clone(values)
}

func cloneAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}

	return maps.Clone(values)
}

func cloneIntMap(values map[string]int) map[string]int {
	if values == nil {
		return nil
	}

	return maps.Clone(values)
}
