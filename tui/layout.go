package tui

// FlexDirection controls flex layout axis.
type FlexDirection int

const (
	FlexRow FlexDirection = iota
	FlexColumn
)

// FlexItem is one flex child.
type FlexItem struct {
	Component Component
	Fixed     int
	Weight    int
}

// Flex lays out children in a row or column.
type Flex struct {
	Direction FlexDirection
	Items     []FlexItem
}

// AddItem appends a flex item.
func (flex *Flex) AddItem(component Component, fixed, weight int) *Flex {
	if flex == nil {
		return flex
	}

	flex.Items = append(flex.Items, FlexItem{Component: component, Fixed: fixed, Weight: weight})

	return flex
}

// Draw draws all flex children.
func (flex *Flex) Draw(screen Screen, rect Rect) {
	if flex == nil || screen == nil || rect.Empty() {
		return
	}

	for index, childRect := range flex.rects(rect) {
		if index < len(flex.Items) && flex.Items[index].Component != nil {
			flex.Items[index].Component.Draw(screen, childRect)
		}
	}
}

func (flex *Flex) rects(rect Rect) []Rect {
	available := rect.Width
	if flex.Direction == FlexColumn {
		available = rect.Height
	}

	fixed := 0
	weight := 0
	for _, item := range flex.Items {
		fixed += max(0, item.Fixed)
		if item.Fixed <= 0 {
			weight += max(1, item.Weight)
		}
	}

	remaining := max(0, available-fixed)
	cursor := 0
	rects := make([]Rect, 0, len(flex.Items))
	for _, item := range flex.Items {
		size := max(0, item.Fixed)
		if item.Fixed <= 0 {
			size = remaining * max(1, item.Weight) / max(1, weight)
		}

		childRect := rect
		if flex.Direction == FlexColumn {
			childRect.Y += cursor
			childRect.Height = size
		} else {
			childRect.X += cursor
			childRect.Width = size
		}
		cursor += size
		rects = append(rects, childRect)
	}

	return rects
}

// GridCell places a component in a grid.
type GridCell struct {
	Component Component
	Row       int
	Column    int
	RowSpan   int
	ColSpan   int
}

// Grid lays out children in equally sized cells.
type Grid struct {
	Rows    int
	Columns int
	Cells   []GridCell
}

// Draw draws all grid cells.
func (grid *Grid) Draw(screen Screen, rect Rect) {
	if grid == nil || screen == nil || rect.Empty() || grid.Rows <= 0 || grid.Columns <= 0 {
		return
	}

	cellWidth := max(1, rect.Width/grid.Columns)
	cellHeight := max(1, rect.Height/grid.Rows)
	for _, cell := range grid.Cells {
		if cell.Component == nil {
			continue
		}
		rowSpan := max(1, cell.RowSpan)
		colSpan := max(1, cell.ColSpan)
		cell.Component.Draw(screen, Rect{
			X:      rect.X + cell.Column*cellWidth,
			Y:      rect.Y + cell.Row*cellHeight,
			Width:  min(rect.Width-cell.Column*cellWidth, cellWidth*colSpan),
			Height: min(rect.Height-cell.Row*cellHeight, cellHeight*rowSpan),
		})
	}
}

// Pages draws one named component at a time.
type Pages struct {
	Pages   map[string]Component
	Current string
}

// Draw draws the current page.
func (pages *Pages) Draw(screen Screen, rect Rect) {
	if pages == nil || pages.Pages == nil {
		return
	}

	page, ok := pages.Pages[pages.Current]
	if ok && page != nil {
		page.Draw(screen, rect)
	}
}

// Window describes a named rectangular component host.
type Window struct {
	Name      string
	Role      string
	Buffer    string
	Renderer  string
	Rect      Rect
	CursorRow int
	CursorCol int
	Visible   bool
	Metadata  map[string]any
}

// Layout describes a named set of windows.
type Layout struct {
	Windows map[string]Window
	Width   int
	Height  int
}
