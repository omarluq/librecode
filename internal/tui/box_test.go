package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestBoxDrawsBorderAndFallsBackToRoundedBorder(t *testing.T) {
	t.Parallel()

	buffer := tui.NewCellBuffer(8, 4, tcell.StyleDefault)
	box := tui.NewBox("T")
	box.Style = tcell.StyleDefault.Foreground(cellcolor.Red)
	box.Draw(buffer, testRect(0, 0, 8, 4))

	require.Equal(t, "╭ T ───╮", bufferLine(buffer, 0))
	require.Equal(t, "│      │", bufferLine(buffer, 1))
	require.Equal(t, "│      │", bufferLine(buffer, 2))
	require.Equal(t, "╰──────╯", bufferLine(buffer, 3))
}

func TestBoxDrawsNarrowBorders(t *testing.T) {
	t.Parallel()

	buffer := tui.NewCellBuffer(1, 3, tcell.StyleDefault)
	tui.NewBox("").Draw(buffer, testRect(0, 0, 1, 3))

	require.Equal(t, "╭", bufferLine(buffer, 0))
	require.Equal(t, "│", bufferLine(buffer, 1))
	require.Equal(t, "╰", bufferLine(buffer, 2))
}
