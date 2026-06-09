package provider

func cloneIntMapForUsage(values map[string]int) map[string]int {
	if len(values) == 0 {
		return nil
	}

	return cloneIntMap(values)
}
