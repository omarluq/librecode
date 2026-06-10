package terminal

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/terminal/panel"
)

func TestPanelRenderOptionsUsesThemeAndKeybindingHints(t *testing.T) {
	t.Parallel()

	bindings := &keybindings{definitions: map[actionID]keyBindingDefinition{
		actionSelectUp:      binding("up", "k"),
		actionSelectDown:    binding("down", "j"),
		actionSelectConfirm: binding("confirm", "enter"),
		actionSelectCancel:  binding("cancel", "esc"),
	}}
	theme := darkTheme()

	options := panelRenderOptions(42, 9, theme, bindings)

	assert.Equal(t, 42, options.Width)
	assert.Equal(t, 9, options.Height)
	assert.Equal(t, panel.Hints{Up: "k", Down: "j", Confirm: "enter", Cancel: "esc"}, options.Hints)
	assert.Equal(t, theme.colors[colorBorder], foreground(t, options.Styles.Border))
	assert.Equal(t, theme.colors[colorAccent], foreground(t, options.Styles.Accent))
	assert.Equal(t, theme.colors[colorMuted], foreground(t, options.Styles.Muted))
	assert.Equal(t, theme.colors[colorText], options.Styles.Text.GetForeground())
	assert.Equal(t, theme.colors[colorDim], foreground(t, options.Styles.Dim))
}

func foreground(t *testing.T, style tcell.Style) tcell.Color {
	t.Helper()

	foreground := style.GetForeground()
	require.NotEqual(t, tcell.ColorDefault, foreground)

	return foreground
}
