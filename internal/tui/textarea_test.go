package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestTextAreaEditing(t *testing.T) {
	t.Parallel()

	area := tui.NewTextArea()
	area.InsertRune('h')
	area.InsertRune('i')
	area.MoveLeft()
	area.InsertRune('!')

	require.Equal(t, "h!i", area.TextValue())
	require.Equal(t, 2, area.CursorValue())
	require.Equal(t, []string{"h", "!", "i"}, area.Chars)

	area.DeleteWordBackward()
	require.Equal(t, "i", area.TextValue())
	require.Equal(t, 0, area.CursorValue())
}

func TestTextAreaClearReturnsPreviousText(t *testing.T) {
	t.Parallel()

	area := tui.NewTextArea()
	area.SetText("draft")

	require.Equal(t, "draft", area.Clear())
	require.True(t, area.Empty())
	require.Empty(t, area.Chars)
}

func TestClampCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cursor    int
		runeCount int
		want      int
	}{
		{name: "below zero", cursor: -1, runeCount: 3, want: 0},
		{name: "inside", cursor: 2, runeCount: 3, want: 2},
		{name: "above length", cursor: 10, runeCount: 3, want: 3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, tui.ClampCursor(test.cursor, test.runeCount))
		})
	}
}

func TestTextAreaCursorAndDeletionMethods(t *testing.T) {
	t.Parallel()

	area := tui.NewTextArea()
	area.SetText("hello\nworld")
	area.MoveLineStart()
	require.Equal(t, 6, area.CursorValue())

	area.MoveRight()
	require.Equal(t, 7, area.CursorValue())

	area.MoveWordRight()
	require.Equal(t, len([]rune("hello\nworld")), area.CursorValue())

	area.MoveWordLeft()
	require.Equal(t, 6, area.CursorValue())

	area.MoveLineEnd()
	require.Equal(t, len([]rune("hello\nworld")), area.CursorValue())

	area.DeleteBackward()
	require.Equal(t, "hello\nworl", area.TextValue())

	area.MoveLineStart()
	area.DeleteForward()
	require.Equal(t, "hello\norl", area.TextValue())

	area.DeleteToLineEnd()
	require.Equal(t, "hello\n", area.TextValue())

	area.MoveLeft()
	area.DeleteToLineStart()
	require.Equal(t, "\n", area.TextValue())

	area.SetText("alpha beta")
	area.MoveLineStart()
	area.DeleteWordForward()
	require.Equal(t, " beta", area.TextValue())
}

func TestTextAreaPrimitivesClampAndHandleBoundaries(t *testing.T) {
	t.Parallel()

	value := []rune("hello\nworld")

	next, cursor := tui.InsertRuneAt(value, -10, 'x')
	require.Equal(t, "xhello\nworld", string(next))
	require.Equal(t, 1, cursor)

	next, cursor = tui.InsertRuneAt(value, 3, 0)
	require.Equal(t, string(value), string(next))
	require.Equal(t, 3, cursor)

	require.Equal(t, 0, tui.MoveCursorLeft(value, -1))
	require.Equal(t, len(value), tui.MoveCursorRight(value, 99))
	require.Equal(t, 6, tui.CurrentLineStart(value, len(value)))
	require.Equal(t, len(value), tui.CurrentLineEnd(value, 6))
	require.Equal(t, 6, tui.WordLeft(value, len(value)))
	require.Equal(t, 5, tui.WordRight(value, 0))

	next, cursor = tui.DeleteBackwardAt(value, 0)
	require.Equal(t, string(value), string(next))
	require.Equal(t, 0, cursor)

	next, cursor = tui.DeleteForwardAt(value, len(value))
	require.Equal(t, string(value), string(next))
	require.Equal(t, len(value), cursor)

	next, cursor = tui.DeleteToLineStartAt(value, len(value))
	require.Equal(t, "hello\n", string(next))
	require.Equal(t, 6, cursor)

	next, cursor = tui.DeleteToLineEndAt(value, 0)
	require.Equal(t, "\nworld", string(next))
	require.Equal(t, 0, cursor)
}

func TestStringChars(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"h", "é"}, tui.StringChars("hé"))
}

func TestTextAreaCursorPositionCountsTrailingSpaces(t *testing.T) {
	t.Parallel()

	row, column := tui.TextAreaCursorPosition([]rune("abc   "), 6, 20)
	require.Equal(t, 0, row)
	require.Equal(t, 6, column)
}

func TestTextAreaBodyLinesPreserveTrailingSpaces(t *testing.T) {
	t.Parallel()

	lines := tui.TextAreaBodyLines([]rune("abc   "), 20)
	require.Equal(t, []string{"abc   "}, lines)
}

func TestTextAreaCursorPositionUsesCellWidth(t *testing.T) {
	t.Parallel()

	_, column := tui.TextAreaCursorPosition([]rune("語 "), 2, 20)
	require.Equal(t, 3, column)
}

func TestTextAreaWordDeletion(t *testing.T) {
	t.Parallel()

	value := []rune("hello  world")
	got, cursor := tui.DeleteWordBackwardAt(value, len(value))
	require.Equal(t, "hello  ", string(got))
	require.Equal(t, 7, cursor)

	got, cursor = tui.DeleteWordForwardAt([]rune("  hello world"), 0)
	require.Equal(t, " world", string(got))
	require.Equal(t, 0, cursor)
}

func TestTextAreaVisibleLinesKeepsCursorVisible(t *testing.T) {
	t.Parallel()

	visible, skipped := tui.VisibleLines([]string{"a", "b", "c", "d"}, 2, 3)
	require.Equal(t, []string{"c", "d"}, visible)
	require.Equal(t, 2, skipped)
}

func TestRenderTextArea(t *testing.T) {
	t.Parallel()

	area := tui.NewTextArea()
	area.SetText("first\nsecond")
	area.Cursor = len([]rune("first\nse"))
	area.Label = "PROMPT"
	rendered := area.Render(12, 2, tui.TextAreaStyles{Border: tcell.StyleDefault, Body: tcell.StyleDefault})

	require.Len(t, rendered.Lines, 4)
	require.Equal(t, 4, rendered.CursorCol)
	require.Equal(t, 2, rendered.CursorRow)
}

func TestRenderTextAreaWrapsBeforeRightBorder(t *testing.T) {
	t.Parallel()

	area := tui.NewTextArea()
	area.SetText("abcdefghijklmnopq")
	area.Label = "normal"
	rendered := area.Render(20, 3, tui.TextAreaStyles{Border: tcell.StyleDefault, Body: tcell.StyleDefault})

	require.Equal(t, "│ abcdefghijklmnop │", rendered.Lines[1].Text)
	require.Equal(t, "│ q                │", rendered.Lines[2].Text)
	require.Equal(t, 3, rendered.CursorCol)
	require.Equal(t, 2, rendered.CursorRow)
}

func TestRenderTextAreaRightAlignsLabel(t *testing.T) {
	t.Parallel()

	area := tui.NewTextArea()
	area.Label = "normal"
	rendered := area.Render(20, 1, tui.TextAreaStyles{Border: tcell.StyleDefault, Body: tcell.StyleDefault})

	require.Equal(t, "╭──────────normal──╮", rendered.Lines[0].Text)
}

func TestWrapPreserveWhitespaceHandlesNarrowWidth(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{""}, tui.WrapPreserveWhitespace("abc", 0))
}
