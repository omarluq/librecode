package tui

// VirtualListItem describes one visible variable-height row.
type VirtualListItem struct {
	Index     int
	RowOffset int
	Height    int
}

// VirtualListResult describes the visible range of a variable-height list.
type VirtualListResult struct {
	Items     []VirtualListItem
	Start     int
	End       int
	Offset    int
	MaxOffset int
	Total     int
}

// VirtualList returns visible items for a bottom-anchored variable-height list.
func VirtualList(heights []int, viewportHeight, offset int) VirtualListResult {
	metrics := newVirtualListMetrics(heights, viewportHeight, offset)
	if metrics.empty() {
		return VirtualListResult{
			Items:     []VirtualListItem{},
			Start:     0,
			End:       0,
			Offset:    metrics.Offset,
			MaxOffset: metrics.MaxOffset,
			Total:     len(heights),
		}
	}

	items, start, end := visibleVirtualListItems(heights, metrics.WindowStart, metrics.WindowEnd)

	return VirtualListResult{
		Items:     items,
		Start:     start,
		End:       end,
		Offset:    metrics.Offset,
		MaxOffset: metrics.MaxOffset,
		Total:     len(heights),
	}
}

type virtualListMetrics struct {
	WindowStart int
	WindowEnd   int
	Offset      int
	MaxOffset   int
	TotalHeight int
}

func newVirtualListMetrics(heights []int, viewportHeight, offset int) virtualListMetrics {
	viewportHeight = max(0, viewportHeight)
	totalHeight := sumPositiveHeights(heights)
	maxOffset := max(0, totalHeight-viewportHeight)
	offset = min(max(0, offset), maxOffset)
	windowStart := max(0, totalHeight-viewportHeight-offset)

	return virtualListMetrics{
		WindowStart: windowStart,
		WindowEnd:   windowStart + viewportHeight,
		Offset:      offset,
		MaxOffset:   maxOffset,
		TotalHeight: totalHeight,
	}
}

func (metrics virtualListMetrics) empty() bool {
	return metrics.WindowEnd <= metrics.WindowStart || metrics.TotalHeight == 0
}

func visibleVirtualListItems(
	heights []int,
	windowStart int,
	windowEnd int,
) (items []VirtualListItem, startIndex, endIndex int) {
	items = []VirtualListItem{}
	cursor := 0
	startIndex = -1
	endIndex = 0

	for index, height := range heights {
		height = max(0, height)

		nextCursor := cursor + height
		if height > 0 && nextCursor > windowStart && cursor < windowEnd {
			if startIndex == -1 {
				startIndex = index
			}

			endIndex = index + 1
			items = append(items, VirtualListItem{
				Index:     index,
				RowOffset: cursor - windowStart,
				Height:    height,
			})
		}

		cursor = nextCursor
		if cursor >= windowEnd {
			break
		}
	}

	if startIndex == -1 {
		startIndex = 0
	}

	return items, startIndex, endIndex
}

func sumPositiveHeights(heights []int) int {
	total := 0
	for _, height := range heights {
		total += max(0, height)
	}

	return total
}

// Viewport describes a scrollable line viewport.
type Viewport struct {
	Offset int
}

// SliceLines returns the visible line range for height.
func (viewport Viewport) SliceLines(items []Line, height int) []Line {
	return SliceViewport(items, viewport.Offset, height)
}

// SliceViewport returns the visible range for offset and height.
func SliceViewport[T any](items []T, offset, height int) []T {
	if height <= 0 || len(items) == 0 {
		return []T{}
	}

	start := min(max(0, offset), max(0, len(items)-1))
	end := min(start+height, len(items))

	return items[start:end]
}
