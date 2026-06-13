package terminal

import (
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
)

func TestCodeHighlightColorHelpersReturnThemeColors(t *testing.T) {
	t.Parallel()

	theme := darkTheme()

	colors := []tcell.Color{
		codeNameColor(chroma.NameFunction, theme),
		codeNameColor(chroma.NameClass, theme),
		codeNameColor(chroma.NameVariable, theme),
		codeNameColor(chroma.Text, theme),
		codeLiteralColor(chroma.LiteralString, theme),
		codeLiteralColor(chroma.LiteralNumber, theme),
		codeLiteralColor(chroma.Text, theme),
		codeGenericColor(chroma.GenericInserted, theme),
		codeGenericColor(chroma.GenericDeleted, theme),
		codeGenericColor(chroma.GenericHeading, theme),
		codeGenericColor(chroma.Text, theme),
		codeOperatorColor(theme),
		codeTypeColor(theme),
	}
	for _, color := range colors {
		assert.NotEqual(t, tcell.ColorDefault, color)
	}

	assert.Equal(t, codeVariableColor(theme), codeNumberColor(theme))
}

func TestLightThemeCodeColorsDifferFromDarkTheme(t *testing.T) {
	t.Parallel()

	dark := darkTheme()
	light := lightTheme()

	assert.NotEqual(t, codeKeywordColor(dark), codeKeywordColor(light))
	assert.NotEqual(t, codeFunctionColor(dark), codeFunctionColor(light))
	assert.NotEqual(t, codeTypeColor(dark), codeTypeColor(light))
	assert.NotEqual(t, codeVariableColor(dark), codeVariableColor(light))
	assert.NotEqual(t, codeStringColor(dark), codeStringColor(light))
	assert.NotEqual(t, codeOperatorColor(dark), codeOperatorColor(light))
}
