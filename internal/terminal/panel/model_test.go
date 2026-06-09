package panel_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPanelTitleAlpha = "Alpha"

func TestModelFiltersByAllItemFields(t *testing.T) {
	t.Parallel()

	model := panel.New(
		panel.Kind("test"),
		"Pick",
		"type to filter",
		[]panel.Item{
			{Value: "first", Title: testPanelTitleAlpha, Description: "primary", Meta: "[one]"},
			{Value: "second", Title: "Beta", Description: "secondary", Meta: "[two]"},
		},
		true,
	)

	model.AppendQueryRune('s')
	model.AppendQueryRune('e')
	model.AppendQueryRune('c')

	items := model.FilteredItems()
	require.Len(t, items, 1)
	assert.Equal(t, "second", items[0].Value)
	value, ok := model.SelectedValue()
	require.True(t, ok)
	assert.Equal(t, "second", value)
}

func TestModelSelectionWrapsAndClamps(t *testing.T) {
	t.Parallel()

	model := panel.New(
		panel.Kind("test"),
		"Pick",
		"",
		[]panel.Item{
			{Value: "a", Title: testPanelTitleAlpha, Description: "", Meta: ""},
			{Value: "b", Title: "Beta", Description: "", Meta: ""},
		},
		true,
	)

	model.MoveSelection(-1)
	assert.Equal(t, 1, model.SelectedIndex())

	model.SetSelectedIndex(99)
	assert.Equal(t, 1, model.SelectedIndex())

	model.AppendQueryRune('z')
	assert.Equal(t, 0, model.SelectedIndex())
	_, ok := model.SelectedItem()
	assert.False(t, ok)
}

func TestModelHandleKeyUsesInjectedBindings(t *testing.T) {
	t.Parallel()

	model := panel.New(
		panel.Kind("test"),
		"Pick",
		"",
		[]panel.Item{{Value: "a", Title: testPanelTitleAlpha, Description: "", Meta: ""}},
		true,
	)

	action := model.HandleKey(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone), testBindings{
		panel.ActionSelectConfirm: true,
	})

	assert.Equal(t, panel.ActionSelect, action.Type)
	assert.Equal(t, "a", action.Value)
}

func TestModelRenderShowsSelectionHintsAndPosition(t *testing.T) {
	t.Parallel()

	model := panel.New(
		panel.Kind("test"),
		"Pick",
		"subtitle",
		[]panel.Item{{Value: "a", Title: testPanelTitleAlpha, Description: "first", Meta: "[one]"}},
		true,
	)

	options := panel.RenderOptions{
		Styles: panel.Styles{
			Border:   tcell.StyleDefault,
			Accent:   tcell.StyleDefault,
			Muted:    tcell.StyleDefault,
			Text:     tcell.StyleDefault,
			Selected: tcell.StyleDefault.Bold(true),
			Dim:      tcell.StyleDefault,
		},
		Hints:  panel.Hints{Up: "up", Down: "down", Confirm: "enter", Cancel: "esc"},
		Width:  40,
		Height: 8,
	}
	lines := model.Render(&options)

	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}
	assert.Contains(t, texts, "│ Pick                                 │")
	assert.Contains(t, texts, "│ → Alpha [one] — first                │")
	assert.Contains(t, texts, "│ up/down navigate · enter select · es │")
}

type testBindings map[panel.ActionID]bool

func (bindings testBindings) Matches(_ *tcell.EventKey, action panel.ActionID) bool {
	return bindings[action]
}
