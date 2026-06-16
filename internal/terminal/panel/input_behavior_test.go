package panel_test

import (
	"github.com/omarluq/librecode/internal/tui"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/terminal/panel"
)

func TestModelHandleKeyNavigationCancelAndSearch(t *testing.T) {
	t.Parallel()

	items := []tui.ListItem{
		{Value: "a", Title: "Alpha", Description: "", Meta: ""},
		{Value: "b", Title: testPanelTitleBeta, Description: "", Meta: ""},
		{Value: "g", Title: "Gamma", Description: "", Meta: ""},
	}

	model := panel.New(panel.Kind("test"), "Pick", "", items, true)

	action := model.HandleKey(tcell.NewEventKey(tcell.KeyRune, "g", tcell.ModNone), testBindings{})
	assert.Equal(t, panel.ActionNone, action.Type)

	filtered := model.FilteredItems()
	require.Len(t, filtered, 1)
	assert.Equal(t, "g", filtered[0].Value)

	action = model.HandleKey(tcell.NewEventKey(tcell.KeyBackspace, "", tcell.ModNone), testBindings{})
	assert.Equal(t, panel.ActionNone, action.Type)
	require.Len(t, model.FilteredItems(), 3)

	action = model.HandleKey(
		tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone),
		testBindings{panel.ActionSelectDown: true},
	)
	assert.Equal(t, panel.ActionNone, action.Type)
	assert.Equal(t, 1, model.SelectedIndex())

	_ = model.HandleKey(
		tcell.NewEventKey(tcell.KeyPgDn, "", tcell.ModNone),
		testBindings{panel.ActionSelectPageDown: true},
	)
	assert.Equal(t, 2, model.SelectedIndex())

	_ = model.HandleKey(
		tcell.NewEventKey(tcell.KeyUp, "", tcell.ModNone),
		testBindings{panel.ActionSelectUp: true},
	)
	assert.Equal(t, 1, model.SelectedIndex())

	_ = model.HandleKey(
		tcell.NewEventKey(tcell.KeyPgUp, "", tcell.ModNone),
		testBindings{panel.ActionSelectPageUp: true},
	)
	assert.Equal(t, 0, model.SelectedIndex())

	action = model.HandleKey(
		tcell.NewEventKey(tcell.KeyEsc, "", tcell.ModNone),
		testBindings{panel.ActionSelectCancel: true},
	)
	assert.Equal(t, panel.ActionCancel, action.Type)
}

func TestModelHandleKeyIgnoresSearchWhenDisabledOrEventNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event      *tcell.EventKey
		name       string
		searchable bool
	}{
		{
			name:       "search disabled",
			searchable: false,
			event:      tcell.NewEventKey(tcell.KeyRune, "z", tcell.ModNone),
		},
		{
			name:       "nil event",
			searchable: true,
			event:      nil,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := panel.New(
				panel.Kind("test"),
				"Pick",
				"",
				[]tui.ListItem{{Value: "a", Title: "Alpha", Description: "", Meta: ""}},
				testCase.searchable,
			)

			model.HandleKey(testCase.event, testBindings{})
			require.Len(t, model.FilteredItems(), 1)
		})
	}
}

func TestModelAccessorsAndWindowStart(t *testing.T) {
	t.Parallel()

	model := panel.New(
		panel.Kind("kind"),
		"Pick",
		"",
		[]tui.ListItem{
			{Value: "0", Title: "Zero", Description: "", Meta: ""},
			{Value: "1", Title: "One", Description: "", Meta: ""},
			{Value: "2", Title: "Two", Description: "", Meta: ""},
			{Value: "3", Title: "Three", Description: "", Meta: ""},
		},
		true,
	)

	assert.Equal(t, panel.Kind("kind"), model.Kind)
	assert.Len(t, model.Items(), 4)
	model.SetSelectedIndex(3)
	selected, ok := model.SelectedItem()
	require.True(t, ok)
	assert.Equal(t, "3", selected.Value)

	lines := model.Render(&tui.ListRenderOptions{
		Styles: tui.ListStyles{
			Border:   tcell.StyleDefault,
			Accent:   tcell.StyleDefault,
			Muted:    tcell.StyleDefault,
			Text:     tcell.StyleDefault,
			Selected: tcell.StyleDefault,
			Dim:      tcell.StyleDefault,
		},
		Hints:  tui.ListHints{Up: "up", Down: "down", Confirm: "enter", Cancel: "esc"},
		Width:  20,
		Height: 5,
	})

	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}

	assert.Contains(t, texts, "│ → Three          │")
}
