package tui_test

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
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
	lines := list.Render(listOptions(28, 8, tui.ListHints{Up: "↑", Down: "↓", Confirm: testEnter, Cancel: testEsc}))
	rendered := strings.Join(lineTexts(lines), "\n")
	require.Contains(t, rendered, "Pick")
	require.Contains(t, rendered, "Search: tw")
	require.Contains(t, rendered, "→ Beta two")

	buffer := tui.NewCellBuffer(28, 8, tcell.StyleDefault)
	list.Draw(
		buffer,
		testRect(0, 0, 28, 8),
		nil,
		tui.ListHints{Up: "k", Down: "j", Confirm: testEnter, Cancel: testEsc},
	)
	require.Equal(t, '╭', buffer.Cell(0, 0).Rune)

	empty := tui.NewList("Empty", "", []tui.ListItem{testListItem("", "Hidden", "", "")}, true)
	empty.AppendQueryRune('z')
	emptyLines := strings.Join(lineTexts(empty.Render(listOptions(20, 6, emptyListHints()))), "\n")
	require.Contains(t, emptyLines, "No matches")
}

func TestListRowsKeepBorderStyleSeparateFromContentStyle(t *testing.T) {
	t.Parallel()

	border := tcell.StyleDefault.Foreground(cellcolor.Blue)
	accent := tcell.StyleDefault.Foreground(cellcolor.Red)
	selected := tcell.StyleDefault.Foreground(cellcolor.Green)
	list := tui.NewList("Pick", "", []tui.ListItem{testListItem("a", "Alpha", "", "")}, false)

	lines := list.Render(&tui.ListRenderOptions{
		Styles: tui.ListStyles{
			Border:   border,
			Accent:   accent,
			Muted:    tcell.StyleDefault,
			Text:     tcell.StyleDefault,
			Selected: selected,
			Dim:      tcell.StyleDefault,
		},
		Hints:  tui.ListHints{Up: "up", Down: "down", Confirm: testEnter, Cancel: testEsc},
		Width:  16,
		Height: 6,
	})
	require.Equal(t, "│", lines[1].Spans[0].Text)
	require.Equal(t, cellcolor.Blue, lines[1].Spans[0].Style.GetForeground())
	require.Equal(t, cellcolor.Red, lines[1].Spans[1].Style.GetForeground())
	require.Equal(t, cellcolor.Blue, lines[1].Spans[2].Style.GetForeground())

	buffer := tui.NewCellBuffer(16, 6, tcell.StyleDefault)
	list.Draw(buffer, testRect(0, 0, 16, 6), &tui.ListStyles{
		Border:   border,
		Accent:   accent,
		Muted:    tcell.StyleDefault,
		Text:     tcell.StyleDefault,
		Selected: selected,
		Dim:      tcell.StyleDefault,
	}, tui.ListHints{Up: "up", Down: "down", Confirm: testEnter, Cancel: testEsc})
	require.Equal(t, cellcolor.Blue, buffer.Cell(0, 1).Style.GetForeground())
	require.Equal(t, cellcolor.Red, buffer.Cell(2, 1).Style.GetForeground())
	require.Equal(t, cellcolor.Blue, buffer.Cell(15, 1).Style.GetForeground())
	require.Equal(t, cellcolor.Blue, buffer.Cell(0, 3).Style.GetForeground())
	require.Equal(t, cellcolor.Green, buffer.Cell(2, 3).Style.GetForeground())
	require.Equal(t, cellcolor.Blue, buffer.Cell(15, 3).Style.GetForeground())
}

func TestAutocompleteSelectionAndRendering(t *testing.T) {
	t.Parallel()

	items := []tui.ListItem{
		testListItem("auth", "/auth", "show auth status", ""),
		testListItem("session", "/session", "show session", ""),
		testListItem("settings", "/settings", "open settings", ""),
	}
	autocomplete := tui.NewAutocomplete(items)
	items[0].Title = "mutated"

	require.Equal(t, "/auth", autocomplete.Items()[0].Title)
	autocomplete.MoveSelection(-1)
	require.Equal(t, 2, autocomplete.SelectedIndex())

	selected, ok := autocomplete.SelectedItem()
	require.True(t, ok)
	require.Equal(t, "settings", selected.Value)

	autocomplete.SetItems(autocomplete.Items()[:1])
	require.Equal(t, 0, autocomplete.SelectedIndex())

	lines := autocomplete.Render(&tui.AutocompleteRenderOptions{
		Styles: tui.AutocompleteStyles{
			Header:   tcell.StyleDefault,
			Text:     tcell.StyleDefault,
			Selected: tcell.StyleDefault.Bold(true),
		},
		Header:         "  slash commands",
		ItemPrefix:     "  ",
		SelectedPrefix: "› ",
		Width:          32,
		MaxItems:       2,
		LabelWidth:     12,
	})
	require.Len(t, lines, 2)
	require.Equal(t, "  slash commands                ", lines[0].Text)
	require.Contains(t, lines[1].Text, "› /auth")
}
