package rendertext

import (
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/tui"
)

// Cell is one rendered terminal cell.
type Cell = tui.Cell

// Buffer is an in-memory terminal cell buffer.
type Buffer = tui.CellBuffer

// NewBuffer returns a buffer initialized with spaces using style.
func NewBuffer(width, height int, style tcell.Style) *Buffer {
	return tui.NewCellBuffer(width, height, style)
}

// Renderer flushes changed cells to a screen.
type Renderer = tui.Renderer

// NewRenderer returns a screen renderer.
func NewRenderer(screen ContentSetter) *Renderer { return tui.NewRenderer(screen) }
