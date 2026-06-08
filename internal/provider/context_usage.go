package provider

import "maps"

func cloneIntMapForUsage(values map[string]int) map[string]int {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]int, len(values))
	maps.Copy(cloned, values)

	return cloned
}
