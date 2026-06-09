package input_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/terminal/input"
)

func TestRenderEditor(t *testing.T) {
	t.Parallel()

	rendered := input.RenderEditor(
		[]rune("first\nsecond"),
		len([]rune("first\nse")),
		12,
		2,
		tcell.StyleDefault,
		tcell.StyleDefault,
		"PROMPT",
	)

	require.Len(t, rendered.Lines, 4)
	require.Equal(t, 4, rendered.CursorCol)
	require.Equal(t, 2, rendered.CursorRow)
}

func TestWrapTextHandlesNarrowWidth(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{""}, input.WrapText("abc", 0))
}
