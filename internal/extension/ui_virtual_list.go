package extension

type uiVirtualListItem struct {
	Index     int
	RowOffset int
	Height    int
}

type uiVirtualListResult struct {
	Items     []uiVirtualListItem
	Start     int
	End       int
	Offset    int
	MaxOffset int
	Total     int
}

func uiVirtualList(heights []int, viewportHeight, offset int) uiVirtualListResult {
	metrics := newUIVirtualListMetrics(heights, viewportHeight, offset)
	if metrics.empty() {
		return emptyUIVirtualListResult(metrics, len(heights))
	}

	items, start, end := visibleVirtualListItems(heights, metrics.WindowStart, metrics.WindowEnd)

	return uiVirtualListResult{
		Items:     items,
		Start:     start,
		End:       end,
		Offset:    metrics.Offset,
		MaxOffset: metrics.MaxOffset,
		Total:     len(heights),
	}
}

type uiVirtualListMetrics struct {
	WindowStart int
	WindowEnd   int
	Offset      int
	MaxOffset   int
	TotalHeight int
}

func newUIVirtualListMetrics(heights []int, viewportHeight, offset int) uiVirtualListMetrics {
	viewportHeight = positiveInt(viewportHeight)
	totalHeight := sumPositiveHeights(heights)
	maxOffset := positiveInt(totalHeight - viewportHeight)
	offset = clampInt(offset, 0, maxOffset)
	windowStart := positiveInt(totalHeight - viewportHeight - offset)

	return uiVirtualListMetrics{
		WindowStart: windowStart,
		WindowEnd:   windowStart + viewportHeight,
		Offset:      offset,
		MaxOffset:   maxOffset,
		TotalHeight: totalHeight,
	}
}

func (metrics uiVirtualListMetrics) empty() bool {
	return metrics.WindowEnd <= metrics.WindowStart || metrics.TotalHeight == 0
}

func emptyUIVirtualListResult(metrics uiVirtualListMetrics, total int) uiVirtualListResult {
	return uiVirtualListResult{
		Items:     []uiVirtualListItem{},
		Start:     0,
		End:       0,
		Offset:    metrics.Offset,
		MaxOffset: metrics.MaxOffset,
		Total:     total,
	}
}

func visibleVirtualListItems(
	heights []int,
	windowStart int,
	windowEnd int,
) (items []uiVirtualListItem, startIndex, endIndex int) {
	items = []uiVirtualListItem{}
	cursor := 0
	startIndex = -1
	endIndex = 0
	for index, height := range heights {
		height = positiveInt(height)
		nextCursor := cursor + height
		if virtualListItemVisible(height, cursor, nextCursor, windowStart, windowEnd) {
			if startIndex == -1 {
				startIndex = index
			}
			endIndex = index + 1
			items = append(items, uiVirtualListItem{
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

func virtualListItemVisible(height, cursor, nextCursor, windowStart, windowEnd int) bool {
	return height > 0 && nextCursor > windowStart && cursor < windowEnd
}

func uiVirtualListItemsForLua(items []uiVirtualListItem) []any {
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, map[string]any{
			"index":        item.Index,
			"lua_index":    item.Index + 1,
			"row_offset":   item.RowOffset,
			luaFieldHeight: item.Height,
		})
	}

	return values
}

func sumPositiveHeights(heights []int) int {
	total := 0
	for _, height := range heights {
		total += positiveInt(height)
	}

	return total
}

func positiveInt(value int) int {
	if value < 0 {
		return 0
	}

	return value
}
