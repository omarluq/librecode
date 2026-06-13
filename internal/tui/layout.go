package tui

// FlexDirection controls flex layout axis.
type FlexDirection int

const (
	// FlexRow lays out flex children from left to right.
	FlexRow FlexDirection = iota
	// FlexColumn lays out flex children from top to bottom.
	FlexColumn
)

// FlexItem is one flex child.
type FlexItem struct {
	Drawer Drawer
	Fixed  int
	Weight int
}

// Flex lays out children in a row or column.
type Flex struct {
	Items     []FlexItem
	Direction FlexDirection
}

// AddItem appends a flex item.
func (flex *Flex) AddItem(component Drawer, fixed, weight int) *Flex {
	if flex == nil {
		return nil
	}

	flex.Items = append(flex.Items, FlexItem{Drawer: component, Fixed: fixed, Weight: weight})

	return flex
}

// Draw draws all flex children.
func (flex *Flex) Draw(screen ContentSetter, rect Rect) {
	if flex == nil || screen == nil || rect.Empty() {
		return
	}

	for index, childRect := range flex.rects(rect) {
		if index < len(flex.Items) && flex.Items[index].Drawer != nil {
			flex.Items[index].Drawer.Draw(screen, childRect)
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
	assignedFlexible := 0
	flexibleSeen := 0
	flexibleCount := flex.flexibleItemCount()

	rects := make([]Rect, 0, len(flex.Items))
	for _, item := range flex.Items {
		size := max(0, item.Fixed)
		if item.Fixed <= 0 {
			flexibleSeen++
			if flexibleSeen == flexibleCount {
				size = remaining - assignedFlexible
			} else {
				size = remaining * max(1, item.Weight) / max(1, weight)
				assignedFlexible += size
			}
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

func (flex *Flex) flexibleItemCount() int {
	count := 0

	for _, item := range flex.Items {
		if item.Fixed <= 0 {
			count++
		}
	}

	return count
}

// GridCell places a component in a grid.
type GridCell struct {
	Drawer  Drawer
	Row     int
	Column  int
	RowSpan int
	ColSpan int
}

// Grid lays out children in equally sized cells.
type Grid struct {
	Cells   []GridCell
	Rows    int
	Columns int
}

// Draw draws all grid cells.
func (grid *Grid) Draw(screen ContentSetter, rect Rect) {
	if grid == nil || screen == nil || rect.Empty() || grid.Rows <= 0 || grid.Columns <= 0 {
		return
	}

	cellWidth := max(1, rect.Width/grid.Columns)
	cellHeight := max(1, rect.Height/grid.Rows)

	for _, cell := range grid.Cells {
		if !grid.validCell(cell) {
			continue
		}

		cell.Drawer.Draw(screen, grid.cellRect(rect, cell, cellWidth, cellHeight))
	}
}

func (grid *Grid) validCell(cell GridCell) bool {
	return cell.Drawer != nil &&
		cell.Row >= 0 &&
		cell.Row < grid.Rows &&
		cell.Column >= 0 &&
		cell.Column < grid.Columns
}

func (grid *Grid) cellRect(rect Rect, cell GridCell, cellWidth, cellHeight int) Rect {
	rowSpan := max(1, cell.RowSpan)
	colSpan := max(1, cell.ColSpan)

	return Rect{
		X:      rect.X + cell.Column*cellWidth,
		Y:      rect.Y + cell.Row*cellHeight,
		Width:  max(0, min(rect.Width-cell.Column*cellWidth, cellWidth*colSpan)),
		Height: max(0, min(rect.Height-cell.Row*cellHeight, cellHeight*rowSpan)),
	}
}

// Pages draws one named component at a time.
type Pages struct {
	Pages   map[string]Drawer
	Current string
}

// Draw draws the current page.
func (pages *Pages) Draw(screen ContentSetter, rect Rect) {
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
	Metadata  map[string]any
	Name      string
	Role      string
	Buffer    string
	Renderer  string
	Rect      Rect
	CursorRow int
	CursorCol int
	Visible   bool
}

// Layout describes a named set of windows.
type Layout struct {
	Windows map[string]Window
	Width   int
	Height  int
}
