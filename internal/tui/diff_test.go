package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestDiffViewRenderAndDraw(t *testing.T) {
	t.Parallel()

	diff := &tui.DiffView{Style: tcell.StyleDefault, Text: "+add\n-del\n same", Theme: testCodeTheme()}
	lines := diff.Render(20, 10)
	require.Equal(t, []string{"+add", "-del", " same"}, lineTexts(lines))
	require.Equal(t, testCodeTheme().DiffAdd, lines[0].Style.GetForeground())
	require.Equal(t, testCodeTheme().DiffDel, lines[1].Style.GetForeground())

	buffer := tui.NewCellBuffer(10, 2, tcell.StyleDefault)
	(&tui.DiffView{Text: "+added\n-deleted", Style: tcell.StyleDefault, Theme: testCodeTheme()}).Draw(
		buffer,
		testRect(0, 0, 10, 2),
	)
	require.Equal(t, '+', buffer.Cell(0, 0).Rune)
	require.Equal(t, '-', buffer.Cell(0, 1).Rune)
}
