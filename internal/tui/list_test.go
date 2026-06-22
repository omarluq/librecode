package tui_test

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestListModelSelectionRenderingAndDraw(t *testing.T) {
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

	list.ShowDetails = false
	list.AppendQueryRune('b')
	lines := list.Render(listOptions(28, 8, tui.ListHints{Up: "↑", Down: "↓", Confirm: testEnter, Cancel: testEsc}))
	rendered := strings.Join(lineTexts(lines), "\n")
	require.Contains(t, rendered, "Pick")
	require.Contains(t, rendered, "Search: b")
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

func TestListFilteringSelectionAndBackspace(t *testing.T) {
	t.Parallel()

	items := []tui.ListItem{
		testListItem("a", "Alpha", "first", testOne),
		testListItem("b", "Beta", "second", "two"),
		testListItem("g", "Gamma", "third", "three"),
	}
	tests := []struct {
		name      string
		query     string
		wantTitle string
		wantValue string
		wantCount int
	}{
		{
			name:      "title match",
			query:     "g",
			wantTitle: "Gamma",
			wantValue: "g",
			wantCount: 1,
		},
		{
			name:      "description match",
			query:     "second",
			wantTitle: "Beta",
			wantValue: "b",
			wantCount: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			listModel := tui.NewList("Pick", "Choose wisely", items, true)
			for _, char := range testCase.query {
				listModel.AppendQueryRune(char)
			}

			require.Equal(t, testCase.query, listModel.Query())
			require.Len(t, listModel.FilteredItems(), testCase.wantCount)
			selected, ok := listModel.SelectedItem()
			require.True(t, ok)
			require.Equal(t, testCase.wantTitle, selected.Title)

			value, ok := listModel.SelectedValue()
			require.True(t, ok)
			require.Equal(t, testCase.wantValue, value)
			listModel.BackspaceQuery()
			require.Equal(t, testCase.query[:len(testCase.query)-1], listModel.Query())
		})
	}
}

func TestListFuzzyFilterMatchesQueries(t *testing.T) {
	t.Parallel()

	items := []tui.ListItem{
		testListItem("session", "Session", "open session picker", ""),
		testListItem("scoped-models", "Scoped Models", "select scoped model set", ""),
		testListItem("settings", "Settings", "open settings", ""),
	}
	tests := []struct {
		name           string
		query          string
		wantFirstValue string
	}{
		{
			name:           "title prefix",
			query:          "sess",
			wantFirstValue: "session",
		},
		{
			name:           "non-contiguous runes",
			query:          "sm",
			wantFirstValue: "scoped-models",
		},
		{
			name:           "description match",
			query:          "picker",
			wantFirstValue: "session",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			list := tui.NewList("Pick", "", items, true)
			for _, char := range testCase.query {
				list.AppendQueryRune(char)
			}

			filtered := list.FilteredItems()
			require.NotEmpty(t, filtered)
			require.Equal(t, testCase.wantFirstValue, filtered[0].Value)
		})
	}
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
