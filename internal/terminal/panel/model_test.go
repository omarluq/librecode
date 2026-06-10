package panel_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPanelTitleAlpha = "Alpha"
	testPanelTitleBeta  = "Beta"
)

type panelScenario int

const (
	panelScenarioFilter panelScenario = iota
	panelScenarioSelection
	panelScenarioKey
	panelScenarioRender
)

func TestModelCoreBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scenario panelScenario
	}{
		{name: "filters by all item fields", scenario: panelScenarioFilter},
		{name: "selection wraps clamps and clears when filtered out", scenario: panelScenarioSelection},
		{name: "handle key uses injected bindings", scenario: panelScenarioKey},
		{name: "render shows selection hints and position", scenario: panelScenarioRender},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runPanelScenario(t, testCase.scenario)
		})
	}
}

func runPanelScenario(t *testing.T, scenario panelScenario) {
	t.Helper()

	switch scenario {
	case panelScenarioFilter:
		assertPanelFiltering(t)
	case panelScenarioSelection:
		assertPanelSelection(t)
	case panelScenarioKey:
		assertPanelKeyBinding(t)
	case panelScenarioRender:
		assertPanelRendering(t)
	}
}

func assertPanelFiltering(t *testing.T) {
	t.Helper()

	model := newTestPanelModel([]panel.Item{
		{Value: "first", Title: testPanelTitleAlpha, Description: "primary", Meta: "[one]"},
		{Value: "second", Title: testPanelTitleBeta, Description: "secondary", Meta: "[two]"},
	})
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

func assertPanelSelection(t *testing.T) {
	t.Helper()

	model := newTestPanelModel([]panel.Item{
		{Value: "a", Title: testPanelTitleAlpha, Description: "", Meta: ""},
		{Value: "b", Title: testPanelTitleBeta, Description: "", Meta: ""},
	})
	model.MoveSelection(-1)
	assert.Equal(t, 1, model.SelectedIndex())

	model.SetSelectedIndex(99)
	assert.Equal(t, 1, model.SelectedIndex())

	model.AppendQueryRune('z')
	assert.Equal(t, 0, model.SelectedIndex())
	_, ok := model.SelectedItem()
	assert.False(t, ok)
}

func assertPanelKeyBinding(t *testing.T) {
	t.Helper()

	model := newTestPanelModel([]panel.Item{{Value: "a", Title: testPanelTitleAlpha, Description: "", Meta: ""}})
	action := model.HandleKey(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone), testBindings{
		panel.ActionSelectConfirm: true,
	})

	assert.Equal(t, panel.ActionSelect, action.Type)
	assert.Equal(t, "a", action.Value)
}

func assertPanelRendering(t *testing.T) {
	t.Helper()

	model := newTestPanelModel([]panel.Item{
		{Value: "a", Title: testPanelTitleAlpha, Description: "first", Meta: "[one]"},
	})
	texts := renderPanelTexts(model)
	assert.Contains(t, texts, "│ Pick                                 │")
	assert.Contains(t, texts, "│ → Alpha [one] — first                │")
	assert.Contains(t, texts, "│ up/down navigate · enter select · es │")
}

func renderPanelTexts(model *panel.Model) []string {
	lines := model.Render(&panel.RenderOptions{
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
	})
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}

	return texts
}

func newTestPanelModel(items []panel.Item) *panel.Model {
	return panel.New(panel.Kind("test"), "Pick", "type to filter", items, true)
}

type testBindings map[panel.ActionID]bool

func (bindings testBindings) Matches(_ *tcell.EventKey, action panel.ActionID) bool {
	return bindings[action]
}
