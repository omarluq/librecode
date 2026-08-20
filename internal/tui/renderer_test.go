package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestRendererFlushesCombiningRunesAndSkipsUnchangedCells(t *testing.T) {
	t.Parallel()

	screen := &cellRecordingScreen{calls: nil}
	style := tcell.StyleDefault
	renderer := tui.NewRenderer(screen)
	frame := tui.NewCellBuffer(1, 1, style)
	frame.SetContent(0, 0, 'e', []rune{'\u0301'}, style)

	renderer.Flush(frame)
	renderer.Flush(frame)

	require.Len(t, screen.calls, 1)
	require.Equal(t, 'e', screen.calls[0].primary)
	require.Equal(t, []rune{'\u0301'}, screen.calls[0].combining)
}

func TestRendererFlushWritesOnlyChangedCells(t *testing.T) {
	t.Parallel()

	screen := &recordingScreen{cells: map[[2]int]rune{}}
	style := tcell.StyleDefault
	current := tui.NewCellBuffer(2, 1, style)
	current.SetContent(1, 0, 'x', nil, style)

	renderer := tui.NewRenderer(screen)
	renderer.Flush(current)

	assert.Equal(t, 'x', screen.cells[[2]int{1, 0}])
}

type benchmarkScreen struct{}

func (screen *benchmarkScreen) SetContent(_, _ int, _ rune, _ []rune, _ tcell.Style) {}

// benchmarkSize builds a representative frame with bordered rows, styled
// text lines, and one combining-rune cell so the diff path stays realistic.
func benchmarkSize(width, height int) *tui.CellBuffer {
	accent := tcell.StyleDefault.Foreground(cellcolor.White).Bold(true)

	frame := tui.NewCellBuffer(width, height, tcell.StyleDefault)
	for row := range height {
		for column := range width {
			switch {
			case row == 0 || row == height-1 || column == 0 || column == width-1:
				frame.SetContent(column, row, '-', nil, accent)
			case row%3 == 0:
				frame.SetContent(column, row, 'x', nil, accent)
			case row%3 == 1:
				frame.SetContent(column, row, 'o', nil, tcell.StyleDefault)
			}
		}
	}

	frame.SetContent(1, 1, 'e', []rune{'\u0301'}, tcell.StyleDefault)

	return frame
}

// benchmarkFrame builds a representative 120x50 frame.
func benchmarkFrame() *tui.CellBuffer {
	return benchmarkSize(120, 50)
}

// BenchmarkRendererFlushSteadyState measures the unchanged-frame flush, the
// common case during idle or redraws that produce identical output.
func BenchmarkRendererFlushSteadyState(b *testing.B) {
	frame := benchmarkFrame()
	renderer := tui.NewRenderer(&benchmarkScreen{})
	renderer.Flush(frame)

	b.ReportAllocs()

	for b.Loop() {
		renderer.Flush(frame)
	}
}

// BenchmarkRendererFlushAllChanged measures the diff path plus the per-flush
// snapshot store, with a small change between frames.
func BenchmarkRendererFlushAllChanged(b *testing.B) {
	renderer := tui.NewRenderer(&benchmarkScreen{})
	frame := benchmarkFrame()
	frame.SetContent(60, 25, 'z', nil, tcell.StyleDefault)
	renderer.Flush(benchmarkFrame())

	b.ReportAllocs()

	current := 'y'
	for b.Loop() {
		frame.SetContent(60, 25, current, nil, tcell.StyleDefault)
		renderer.Flush(frame)

		if current == 'y' {
			current = 'z'
		} else {
			current = 'y'
		}
	}
}

// BenchmarkRendererFlushResize measures the force-redraw path taken after
// terminal resizes between a wide and a narrow frame.
func BenchmarkRendererFlushResize(b *testing.B) {
	renderer := tui.NewRenderer(&benchmarkScreen{})
	wide := benchmarkSize(120, 50)
	narrow := benchmarkSize(80, 50)

	renderer.Flush(wide)

	b.ReportAllocs()

	for b.Loop() {
		renderer.Flush(narrow)
		renderer.Flush(wide)
	}
}

// flushWrite records a single screen write performed by Flush.
type flushWrite struct {
	style     tcell.Style
	combining []rune
	x         int
	y         int
	primary   rune
}

// newFlushWrite pins every field so assertions compare full writes.
func newFlushWrite(x, y int, primary rune, combining []rune, style tcell.Style) flushWrite {
	return flushWrite{x: x, y: y, primary: primary, combining: combining, style: style}
}

type flushRecordingScreen struct {
	writes []flushWrite
}

func (screen *flushRecordingScreen) SetContent(x, y int, primary rune, combining []rune, style tcell.Style) {
	screen.writes = append(screen.writes, newFlushWrite(x, y, primary, append([]rune(nil), combining...), style))
}

// drawFrame writes a small 4x3 sample frame with a border, styled glyphs, and
// one combining-rune cell.
func drawFrame(buffer *tui.CellBuffer, combineE bool, runeAt11 rune, styleAt11 tcell.Style) {
	accent := tcell.StyleDefault.Foreground(cellcolor.White).Bold(true)

	buffer.SetContent(0, 0, '┌', nil, accent)
	buffer.SetContent(1, 0, '─', nil, accent)
	buffer.SetContent(3, 2, '┘', nil, accent)

	var combining []rune
	if combineE {
		combining = []rune{'\u0301'}
	}

	buffer.SetContent(1, 1, runeAt11, combining, styleAt11)
}

// expectedFirstFlush returns the writes a first Flush of drawFrame's layout
// must produce: every cell, in scan order, blanks included.
func expectedFirstFlush() []flushWrite {
	plain := tcell.StyleDefault
	accent := tcell.StyleDefault.Foreground(cellcolor.White).Bold(true)

	writes := make([]flushWrite, 0, 12)

	for row := range 3 {
		for column := range 4 {
			primary, style := ' ', plain

			switch {
			case column == 0 && row == 0:
				primary, style = '┌', accent
			case column == 1 && row == 0:
				primary, style = '─', accent
			case column == 3 && row == 2:
				primary, style = '┘', accent
			}

			writes = append(writes, newFlushWrite(column, row, primary, nil, style))
		}
	}

	writes[5] = newFlushWrite(1, 1, 'e', []rune{'\u0301'}, plain)

	return writes
}

func TestRendererFlushOutputSequence(t *testing.T) {
	t.Parallel()

	plain := tcell.StyleDefault
	accent := tcell.StyleDefault.Foreground(cellcolor.White).Bold(true)
	screen := &flushRecordingScreen{writes: nil}
	renderer := tui.NewRenderer(screen)

	// The first flush writes every cell because there is no previous frame.
	frame := tui.NewCellBuffer(4, 3, plain)
	drawFrame(frame, true, 'e', plain)
	renderer.Flush(frame)
	require.Equal(t, expectedFirstFlush(), screen.writes)

	// An identical frame must produce no writes.
	same := tui.NewCellBuffer(4, 3, plain)
	drawFrame(same, true, 'e', plain)
	renderer.Flush(same)
	assert.Empty(t, screen.writes[len(expectedFirstFlush()):])

	// Only the changed cell is written; the combining rune is dropped.
	changed := tui.NewCellBuffer(4, 3, plain)
	drawFrame(changed, false, 'E', accent)
	renderer.Flush(changed)
	assert.Equal(t, []flushWrite{newFlushWrite(1, 1, 'E', nil, accent)}, screen.writes[len(expectedFirstFlush()):])

	// A resize forces every cell to be rewritten.
	before := len(screen.writes)
	resized := tui.NewCellBuffer(2, 2, plain)
	resized.SetContent(0, 0, 'a', nil, accent)
	renderer.Flush(resized)
	assert.Equal(t, []flushWrite{
		newFlushWrite(0, 0, 'a', nil, accent),
		newFlushWrite(1, 0, ' ', nil, plain),
		newFlushWrite(0, 1, ' ', nil, plain),
		newFlushWrite(1, 1, ' ', nil, plain),
	}, screen.writes[before:])

	// Shrinking back reuses the larger previous buffer and only writes the
	// cells that differ, proving stale rows are neither compared nor written.
	before = len(screen.writes)
	blank := tui.NewCellBuffer(2, 2, plain)
	renderer.Flush(blank)
	assert.Equal(t, []flushWrite{newFlushWrite(0, 0, ' ', nil, plain)}, screen.writes[before:])
}

func TestRendererFlushKeepsPreviousFrameIndependent(t *testing.T) {
	t.Parallel()

	style := tcell.StyleDefault
	frame := tui.NewCellBuffer(2, 2, style)
	frame.SetContent(0, 0, 'e', []rune{'\u0301'}, style)

	screen := &flushRecordingScreen{writes: nil}
	renderer := tui.NewRenderer(screen)

	// The first flush writes every cell; only the combining-rune cell matters
	// for this assertion, so pin it before mutating the frame.
	renderer.Flush(frame)
	require.Equal(t, newFlushWrite(0, 0, 'e', []rune{'\u0301'}, style), screen.writes[0])
	firstFlushWrites := len(screen.writes)

	// Mutating the flushed frame must not alter the renderer's snapshot.
	frame.SetContent(0, 0, 'z', nil, style)
	frame.SetContent(1, 0, 'y', nil, style)
	renderer.Flush(frame)
	assert.Equal(t, []flushWrite{
		newFlushWrite(0, 0, 'z', nil, style),
		newFlushWrite(1, 0, 'y', nil, style),
	}, screen.writes[firstFlushWrites:])
}
