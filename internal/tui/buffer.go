package tui

import (
	"sync"

	"github.com/gdamore/tcell/v3"
)

// Cell is one rendered terminal cell.
type Cell struct {
	Style tcell.Style
	Comb  []rune
	Rune  rune
}

// Equal reports whether two cells contain the same glyph and style.
func (cell Cell) Equal(other Cell) bool {
	if cell.Rune != other.Rune || cell.Style != other.Style || len(cell.Comb) != len(other.Comb) {
		return false
	}

	for index, combiner := range cell.Comb {
		if combiner != other.Comb[index] {
			return false
		}
	}

	return true
}

// CellBuffer is an in-memory terminal cell buffer.
type CellBuffer struct {
	cells  []Cell
	width  int
	height int
}

// NewCellBuffer returns a buffer initialized with spaces using style.
func NewCellBuffer(width, height int, style tcell.Style) *CellBuffer {
	width = max(0, width)
	height = max(0, height)

	buffer := &CellBuffer{
		cells:  make([]Cell, width*height),
		width:  width,
		height: height,
	}

	fill := Cell{Rune: ' ', Comb: nil, Style: style}
	for index := range buffer.cells {
		buffer.cells[index] = fill
	}

	return buffer
}

// Width returns the buffer width.
func (buffer *CellBuffer) Width() int {
	if buffer == nil {
		return 0
	}

	return buffer.width
}

// Height returns the buffer height.
func (buffer *CellBuffer) Height() int {
	if buffer == nil {
		return 0
	}

	return buffer.height
}

// SetContent implements ContentSetter.
func (buffer *CellBuffer) SetContent(column, row int, mainc rune, combc []rune, style tcell.Style) {
	if buffer == nil || column < 0 || row < 0 || column >= buffer.width || row >= buffer.height {
		return
	}

	if mainc == 0 {
		mainc = ' '
	}

	buffer.cells[buffer.offset(column, row)] = Cell{
		Rune:  mainc,
		Comb:  append([]rune(nil), combc...),
		Style: style,
	}
}

// Cell returns a rendered cell. Callers should pass in-bounds coordinates.
func (buffer *CellBuffer) Cell(column, row int) Cell {
	return buffer.cells[buffer.offset(column, row)]
}

func (buffer *CellBuffer) offset(column, row int) int {
	return row*buffer.width + column
}

// Clone returns a deep copy of the buffer.
func (buffer *CellBuffer) Clone() *CellBuffer {
	if buffer == nil {
		return nil
	}

	cloned := &CellBuffer{
		cells:  make([]Cell, len(buffer.cells)),
		width:  buffer.width,
		height: buffer.height,
	}
	for index, cell := range buffer.cells {
		cloned.cells[index] = Cell{
			Rune:  cell.Rune,
			Comb:  append([]rune(nil), cell.Comb...),
			Style: cell.Style,
		}
	}

	return cloned
}

// Renderer flushes changed cells to a screen.
type Renderer struct {
	screen   ContentSetter
	previous *CellBuffer
	Lexer    LexerEngine
	Markdown MarkdownEngine
}

// NewRenderer returns a screen renderer.
func NewRenderer(screen ContentSetter) *Renderer {
	return &Renderer{
		screen:   screen,
		previous: nil,
		Markdown: MarkdownEngine{parser: nil, once: sync.Once{}},
		Lexer:    NewLexerEngine(),
	}
}

// Flush writes changed cells from frame to the screen.
func (renderer *Renderer) Flush(frame *CellBuffer) {
	if renderer == nil || renderer.screen == nil || frame == nil {
		return
	}

	force := renderer.previous == nil ||
		renderer.previous.width != frame.width ||
		renderer.previous.height != frame.height
	for y := range frame.height {
		for x := range frame.width {
			cell := frame.Cell(x, y)
			if force || !cell.Equal(renderer.previous.Cell(x, y)) {
				renderer.screen.SetContent(x, y, cell.Rune, cell.Comb, cell.Style)
			}
		}
	}

	renderer.previous = frame.Clone()
}
