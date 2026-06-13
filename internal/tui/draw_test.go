package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestDrawTextLineAndLines(t *testing.T) {
	t.Parallel()

	buffer := tui.NewCellBuffer(8, 3, tcell.StyleDefault)
	tui.DrawText(buffer, testRect(1, 0, 4, 1), tcell.StyleDefault, testHello)
	tui.DrawLine(buffer, testRect(0, 1, 4, 1), tui.Line{
		Text:  "abcd",
		Style: tcell.StyleDefault,
		Spans: []tui.Span{{Text: "ab", Style: tcell.StyleDefault}, {Text: "cd", Style: tcell.StyleDefault}},
	})
	tui.DrawLines(buffer, testRect(0, 2, 8, 1), []tui.Line{tui.NewLine(tcell.StyleDefault, "line")})

	require.Equal(t, " hell   ", bufferLine(buffer, 0))
	require.Equal(t, "abcd    ", bufferLine(buffer, 1))
	require.Equal(t, "line    ", bufferLine(buffer, 2))
}

func TestWriteCellsNoFillAndSegmentBounds(t *testing.T) {
	t.Parallel()

	buffer := tui.NewCellBuffer(4, 1, tcell.StyleDefault)
	used := tui.WriteCellsNoFill(buffer, 0, 0, 4, "語x", tcell.StyleDefault)
	require.Equal(t, 3, used)
	require.Equal(t, '語', buffer.Cell(0, 0).Rune)
	require.Equal(t, ' ', buffer.Cell(1, 0).Rune)
	require.Equal(t, 'x', buffer.Cell(2, 0).Rune)

	used = tui.WriteCellsNoFill(buffer, 0, 0, 1, "語", tcell.StyleDefault)
	require.Equal(t, 0, used)

	used = tui.WriteCellsNoFill(buffer, 0, 0, 4, "a\u0301", tcell.StyleDefault)
	require.Equal(t, 1, used)
}

func TestWriteCellsHandlesWideRunesTabsAndFill(t *testing.T) {
	t.Parallel()

	buffer := tui.NewCellBuffer(8, 1, tcell.StyleDefault)
	used := tui.WriteCells(buffer, 0, 0, 8, "語\tx", tcell.StyleDefault.Bold(true))

	require.Equal(t, 8, used)
	require.Equal(t, '語', buffer.Cell(0, 0).Rune)
	require.Equal(t, ' ', buffer.Cell(1, 0).Rune)
	require.Equal(t, ' ', buffer.Cell(2, 0).Rune)
	require.Equal(t, ' ', buffer.Cell(5, 0).Rune)
	require.Equal(t, 'x', buffer.Cell(6, 0).Rune)
	require.Equal(t, ' ', buffer.Cell(7, 0).Rune)
}
