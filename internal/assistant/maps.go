package assistant

import "github.com/omarluq/librecode/internal/mapsutil"

func cloneStringMap(values map[string]string) map[string]string {
	return mapsutil.CloneOrEmpty(values)
}

func cloneAnyMap(values map[string]any) map[string]any {
	return mapsutil.CloneOrEmpty(values)
}

func cloneIntAnyMap(values map[string]int) map[string]any {
	return mapsutil.IntMapToAnyMap(values)
}
