package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestViewportAndVirtualList(t *testing.T) {
	t.Parallel()

	result := tui.VirtualList([]int{2, -1, 3, 1}, 3, 1)
	require.Equal(t, 4, result.Total)
	require.Equal(t, 3, result.MaxOffset)
	require.Equal(t, 1, result.Offset)
	require.Equal(t, []tui.VirtualListItem{{Index: 2, RowOffset: 0, Height: 3}}, result.Items)
	require.Equal(t, 2, result.Start)
	require.Equal(t, 3, result.End)

	empty := tui.VirtualList(nil, 3, 0)
	require.Empty(t, empty.Items)
	require.Zero(t, empty.Total)

	lines := []tui.Line{
		tui.NewLine(tcell.StyleDefault, "a"),
		tui.NewLine(tcell.StyleDefault, "b"),
		tui.NewLine(tcell.StyleDefault, "c"),
	}
	require.Equal(t, []tui.Line{lines[1], lines[2]}, (tui.Viewport{Offset: 99}).SliceLines(lines, 2))
	require.Equal(t, []int{1, 2, 3}, tui.SliceViewport([]int{1, 2, 3}, 1, 9))
	require.Empty(t, tui.SliceViewport([]int{1, 2}, 0, 0))
}
