package terminal

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
)

func TestCodeThemeUsesTerminalThemeColors(t *testing.T) {
	t.Parallel()

	theme := darkTheme()
	codeTheme := codeTheme(theme)

	colors := []tcell.Color{
		codeTheme.Text,
		codeTheme.Accent,
		codeTheme.Success,
		codeTheme.Warning,
		codeTheme.Dim,
		codeTheme.Muted,
		codeTheme.DiffAdd,
		codeTheme.DiffDel,
	}
	for _, color := range colors {
		assert.NotEqual(t, tcell.ColorDefault, color)
	}
}

func TestLightThemeCodeThemeDiffersFromDarkTheme(t *testing.T) {
	t.Parallel()

	dark := codeTheme(darkTheme())
	light := codeTheme(lightTheme())

	assert.NotEqual(t, dark.Accent, light.Accent)
	assert.NotEqual(t, dark.Success, light.Success)
	assert.NotEqual(t, dark.Warning, light.Warning)
	assert.NotEqual(t, dark.Text, light.Text)
	assert.NotEqual(t, dark.Dim, light.Dim)
}
