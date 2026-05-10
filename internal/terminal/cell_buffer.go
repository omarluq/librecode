package terminal

import "github.com/gdamore/tcell/v3"

type cellTarget interface {
	SetContent(column, row int, mainc rune, combc []rune, style tcell.Style)
}

type screenCell struct {
	Style tcell.Style
	Rune  rune
}

type cellBuffer struct {
	cells  []screenCell
	width  int
	height int
}

func newCellBuffer(width, height int, style tcell.Style) *cellBuffer {
	buffer := &cellBuffer{
		cells:  make([]screenCell, max(0, width*height)),
		width:  max(0, width),
		height: max(0, height),
	}
	fill := screenCell{Style: style, Rune: ' '}
	for index := range buffer.cells {
		buffer.cells[index] = fill
	}

	return buffer
}

func (buffer *cellBuffer) SetContent(column, row int, mainc rune, _ []rune, style tcell.Style) {
	if buffer == nil || column < 0 || row < 0 || column >= buffer.width || row >= buffer.height {
		return
	}
	if mainc == 0 {
		mainc = ' '
	}
	buffer.cells[buffer.offset(column, row)] = screenCell{Style: style, Rune: mainc}
}

func (buffer *cellBuffer) cell(column, row int) screenCell {
	return buffer.cells[buffer.offset(column, row)]
}

func (buffer *cellBuffer) offset(column, row int) int {
	return row*buffer.width + column
}

func (buffer *cellBuffer) clone() *cellBuffer {
	cloned := &cellBuffer{
		cells:  make([]screenCell, len(buffer.cells)),
		width:  buffer.width,
		height: buffer.height,
	}
	copy(cloned.cells, buffer.cells)

	return cloned
}

type screenRenderer struct {
	screen   tcell.Screen
	previous *cellBuffer
}

func newScreenRenderer(screen tcell.Screen) *screenRenderer {
	return &screenRenderer{screen: screen, previous: nil}
}

func (renderer *screenRenderer) flush(frame *cellBuffer) {
	if renderer == nil || renderer.screen == nil || frame == nil {
		return
	}
	force := renderer.previous == nil ||
		renderer.previous.width != frame.width ||
		renderer.previous.height != frame.height
	for y := range frame.height {
		for x := range frame.width {
			cell := frame.cell(x, y)
			if force || cell != renderer.previous.cell(x, y) {
				renderer.screen.SetContent(x, y, cell.Rune, nil, cell.Style)
			}
		}
	}
	renderer.previous = frame.clone()
}
