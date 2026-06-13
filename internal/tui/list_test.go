package tui_test

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestListModelFilteringSelectionRenderingAndDraw(t *testing.T) {
	t.Parallel()

	items := []tui.ListItem{
		testListItem("a", "Alpha", "first", testOne),
		testListItem("b", "Beta", "second", "two"),
		testListItem("g", "Gamma", "third", "three"),
	}
	list := tui.NewList("Pick", "Choose wisely", items, true)
	items[0].Title = "mutated"
	require.Equal(t, "Alpha", list.Items()[0].Title)
	require.True(t, list.Searchable())
	require.Empty(t, list.Query())

	list.SetSelectedIndex(99)
	require.Equal(t, 2, list.SelectedIndex())
	list.MoveSelection(1)
	require.Equal(t, 0, list.SelectedIndex())
	list.MoveSelection(-1)
	require.Equal(t, 2, list.SelectedIndex())

	list.AppendQueryRune('t')
	list.AppendQueryRune('w')
	require.Equal(t, "tw", list.Query())
	require.Len(t, list.FilteredItems(), 1)
	selected, ok := list.SelectedItem()
	require.True(t, ok)
	require.Equal(t, "Beta", selected.Title)

	value, ok := list.SelectedValue()
	require.True(t, ok)
	require.Equal(t, "b", value)
	list.BackspaceQuery()
	require.Equal(t, "t", list.Query())

	list.ShowDetails = false
	list.AppendQueryRune('w')
	lines := list.Render(listOptions(28, 8, tui.ListHints{Up: "↑", Down: "↓", Confirm: "enter", Cancel: "esc"}))
	rendered := strings.Join(lineTexts(lines), "\n")
	require.Contains(t, rendered, "Pick")
	require.Contains(t, rendered, "Search: tw")
	require.Contains(t, rendered, "→ Beta two")

	buffer := tui.NewCellBuffer(28, 8, tcell.StyleDefault)
	list.Draw(buffer, testRect(0, 0, 28, 8), nil, tui.ListHints{Up: "k", Down: "j", Confirm: "enter", Cancel: "esc"})
	require.Equal(t, '╭', buffer.Cell(0, 0).Rune)

	empty := tui.NewList("Empty", "", []tui.ListItem{testListItem("", "Hidden", "", "")}, true)
	empty.AppendQueryRune('z')
	emptyLines := strings.Join(lineTexts(empty.Render(listOptions(20, 6, emptyListHints()))), "\n")
	require.Contains(t, emptyLines, "No matches")
}

func TestAutocompleteCreatesList(t *testing.T) {
	t.Parallel()

	autocomplete := tui.NewAutocomplete([]tui.ListItem{testListItem("", "Command", "", "")})
	require.NotNil(t, autocomplete.List)
}
