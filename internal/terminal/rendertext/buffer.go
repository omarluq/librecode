package rendertext

import "github.com/gdamore/tcell/v3"

// Cell is one rendered terminal cell.
type Cell struct {
	Style tcell.Style
	Rune  rune
}

// Buffer is an in-memory terminal cell buffer.
type Buffer struct {
	cells  []Cell
	width  int
	height int
}

// NewBuffer returns a buffer initialized with spaces using style.
func NewBuffer(width, height int, style tcell.Style) *Buffer {
	buffer := &Buffer{
		cells:  make([]Cell, max(0, width*height)),
		width:  max(0, width),
		height: max(0, height),
	}

	fill := Cell{Style: style, Rune: ' '}
	for index := range buffer.cells {
		buffer.cells[index] = fill
	}

	return buffer
}

// Width returns the buffer width.
func (buffer *Buffer) Width() int {
	if buffer == nil {
		return 0
	}

	return buffer.width
}

// Height returns the buffer height.
func (buffer *Buffer) Height() int {
	if buffer == nil {
		return 0
	}

	return buffer.height
}

// SetContent implements ContentSetter.
func (buffer *Buffer) SetContent(column, row int, mainc rune, _ []rune, style tcell.Style) {
	if buffer == nil || column < 0 || row < 0 || column >= buffer.width || row >= buffer.height {
		return
	}

	if mainc == 0 {
		mainc = ' '
	}

	buffer.cells[buffer.offset(column, row)] = Cell{Style: style, Rune: mainc}
}

// Cell returns a rendered cell. Callers should pass in-bounds coordinates.
func (buffer *Buffer) Cell(column, row int) Cell {
	return buffer.cells[buffer.offset(column, row)]
}

func (buffer *Buffer) offset(column, row int) int {
	return row*buffer.width + column
}

// Clone returns a deep copy of the buffer.
func (buffer *Buffer) Clone() *Buffer {
	cloned := &Buffer{
		cells:  make([]Cell, len(buffer.cells)),
		width:  buffer.width,
		height: buffer.height,
	}
	copy(cloned.cells, buffer.cells)

	return cloned
}

// ContentScreen receives terminal cell updates.
type ContentScreen interface {
	SetContent(column, row int, mainc rune, combc []rune, style tcell.Style)
}

// Renderer flushes changed cells to a screen.
type Renderer struct {
	screen   ContentScreen
	previous *Buffer
}

// NewRenderer returns a screen renderer.
func NewRenderer(screen ContentScreen) *Renderer {
	return &Renderer{screen: screen, previous: nil}
}

// Flush writes changed cells from frame to the screen.
func (renderer *Renderer) Flush(frame *Buffer) {
	if renderer == nil || renderer.screen == nil || frame == nil {
		return
	}

	force := renderer.previous == nil ||
		renderer.previous.width != frame.width ||
		renderer.previous.height != frame.height
	for y := range frame.height {
		for x := range frame.width {
			cell := frame.Cell(x, y)
			if force || cell != renderer.previous.Cell(x, y) {
				renderer.screen.SetContent(x, y, cell.Rune, nil, cell.Style)
			}
		}
	}

	renderer.previous = frame.Clone()
}
