package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
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
