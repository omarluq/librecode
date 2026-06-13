package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestFlexDrawsRowsAndColumns(t *testing.T) {
	t.Parallel()

	first := &rectRecorder{rects: nil}
	second := &rectRecorder{rects: nil}
	flex := (&tui.Flex{Items: nil, Direction: tui.FlexRow}).AddItem(first, 3, 0).AddItem(second, 0, 1)
	flex.Draw(tui.NewCellBuffer(10, 2, tcell.StyleDefault), testRect(0, 0, 10, 2))
	require.Equal(t, testRect(0, 0, 3, 2), first.rects[0])
	require.Equal(t, testRect(3, 0, 7, 2), second.rects[0])

	columnChild := &rectRecorder{rects: nil}
	(&tui.Flex{
		Items:     []tui.FlexItem{{Drawer: columnChild, Fixed: 0, Weight: 1}},
		Direction: tui.FlexColumn,
	}).Draw(tui.NewCellBuffer(4, 6, tcell.StyleDefault), testRect(0, 0, 4, 6))
	require.Equal(t, testRect(0, 0, 4, 6), columnChild.rects[0])
}

func TestGridDrawsValidCellsAndSkipsInvalidCells(t *testing.T) {
	t.Parallel()

	valid := &rectRecorder{rects: nil}
	invalid := &rectRecorder{rects: nil}
	grid := &tui.Grid{Rows: 2, Columns: 2, Cells: []tui.GridCell{
		{Drawer: valid, Row: 1, Column: 1, RowSpan: 1, ColSpan: 2},
		{Drawer: invalid, Row: 9, Column: 0, RowSpan: 0, ColSpan: 0},
	}}
	grid.Draw(tui.NewCellBuffer(10, 4, tcell.StyleDefault), testRect(0, 0, 10, 4))
	require.Equal(t, []tui.Rect{testRect(5, 2, 5, 2)}, valid.rects)
	require.Empty(t, invalid.rects)
}

func TestPagesDrawsCurrentPage(t *testing.T) {
	t.Parallel()

	page := &rectRecorder{rects: nil}
	pages := &tui.Pages{Pages: map[string]tui.Drawer{"one": page}, Current: testOne}
	pages.Draw(tui.NewCellBuffer(3, 3, tcell.StyleDefault), testRect(0, 0, 3, 3))
	require.Len(t, page.rects, 1)
}
