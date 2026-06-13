package tui

// Tail returns at most maxItems tail items. Negative maxItems means no limit.
func Tail[T any](items []T, maxItems int) []T {
	if maxItems < 0 || len(items) <= maxItems {
		return items
	}

	return items[len(items)-maxItems:]
}
