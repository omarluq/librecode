package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestModalDrawsCenteredBoxAndChild(t *testing.T) {
	t.Parallel()

	modalChild := &rectRecorder{rects: nil}
	buffer := tui.NewCellBuffer(12, 6, tcell.StyleDefault)
	modal := &tui.Modal{BoxStyle: tcell.StyleDefault, Child: modalChild, Title: "M", Width: 8, Height: 4}
	modal.Draw(buffer, testRect(0, 0, 12, 6))
	require.Equal(t, '╭', buffer.Cell(2, 1).Rune)
	require.Equal(t, testRect(3, 2, 6, 2), modalChild.rects[0])
}

func TestFormRenderAndDraw(t *testing.T) {
	t.Parallel()

	buffer := tui.NewCellBuffer(12, 2, tcell.StyleDefault)
	form := &tui.Form{
		Style:      tcell.StyleDefault,
		LabelStyle: tcell.StyleDefault,
		Title:      "Config",
		Fields:     []tui.FormField{{Label: "Model", Value: "sonnet"}},
	}
	formLines := form.Render(20, 4)
	require.Equal(t, []string{"Config", "Model: sonnet"}, lineTexts(formLines))
	form.Draw(buffer, testRect(0, 0, 12, 2))
}
