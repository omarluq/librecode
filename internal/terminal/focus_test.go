//nolint:testpackage // These tests exercise unexported terminal focus routing helpers.
package terminal

import (
	"context"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFocusStatePrioritizesPanelAndAutocomplete(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	assert.Equal(t, focusKindComposer, app.focusState().Kind)

	app.setComposerText("/s")
	autocompleteFocus := app.focusState()
	assert.Equal(t, focusKindAutocomplete, autocompleteFocus.Kind)
	assert.True(t, autocompleteFocus.Exclusive)

	app.openModelPanel()
	panelFocus := app.focusState()
	assert.Equal(t, focusKindPanel, panelFocus.Kind)
	assert.Equal(t, string(panelModel), panelFocus.PanelKind)
	assert.True(t, panelFocus.Exclusive)
}

func TestFocusedPanelPreventsComposerExtensionKeymap(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.keymap.set({ focus = "composer" }, "down", function()
  librecode.buf.set_text("composer", "stolen")
  librecode.event.consume()
end)
`)
	app.openPanel(newSelectionPanel(panelModel, "Models", "", []panelItem{
		{Value: "one", Title: "one", Description: "", Meta: ""},
		{Value: "two", Title: "two", Description: "", Meta: ""},
	}, true))
	selected := app.panel.selected

	pressTerminalKey(t, app, tcell.KeyDown, "")

	assert.Equal(t, selected+1, app.panel.selected)
	assertEditorText(t, app, "")
}

func TestFocusedAutocompletePreventsComposerExtensionKeymap(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.keymap.set({ focus = "composer" }, "down", function()
  librecode.buf.set_text("composer", "stolen")
  librecode.event.consume()
end)
`)
	app.setComposerText("/s")

	pressTerminalKey(t, app, tcell.KeyDown, "")

	assert.NotZero(t, app.autocompleteSelection)
	assertEditorText(t, app, "/s")
}

func TestFocusedComposerExtensionStillHandlesComposerKeys(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.keymap.set({ focus = "composer" }, "x", function()
  librecode.buf.set_text("composer", "handled")
  librecode.event.consume()
end)
`)

	shouldQuit, err := app.handleKey(context.Background(), tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone))
	require.NoError(t, err)
	assert.False(t, shouldQuit)
	assertEditorText(t, app, "handled")
}
