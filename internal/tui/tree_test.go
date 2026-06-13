package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestTreeRenderAndMarkers(t *testing.T) {
	t.Parallel()

	leaf := testTreeNode("leaf", false, false)
	leaf.Style = tcell.StyleDefault.Foreground(cellcolor.Blue)
	root := testTreeNode("root", true, false,
		testTreeNode("closed", false, false, testTreeNode("hidden", false, false)),
		testTreeNode("open", true, true, leaf),
		testTreeNode("plain", false, false),
	)
	view := &tui.TreeView{
		Style:         tcell.StyleDefault,
		SelectedStyle: tcell.StyleDefault.Reverse(true),
		Root:          root,
		SelectedIndex: 0,
	}

	flattened := view.Flatten()
	require.Equal(t, []string{"root", "  ▸ closed", "  ▾ open", "      leaf", "    plain"}, lineTexts(flattened))
	require.Equal(t, tcell.StyleDefault.Reverse(true), flattened[2].Style)

	rendered := view.Render(8, 3)
	require.Equal(t, []string{"  ▾ open", "      l…", "    pla…"}, lineTexts(rendered))

	buffer := tui.NewCellBuffer(8, 3, tcell.StyleDefault)
	view.Draw(buffer, testRect(0, 0, 8, 3))
	require.Equal(t, '▾', buffer.Cell(2, 0).Rune)
}
