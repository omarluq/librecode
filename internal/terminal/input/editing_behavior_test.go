package input_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/terminal/input"
)

func TestBufferCursorAndDeletionMethods(t *testing.T) {
	t.Parallel()

	buffer := input.NewBuffer()
	buffer.SetText("hello\nworld")
	buffer.MoveLineStart()
	require.Equal(t, 6, buffer.CursorValue())

	buffer.MoveRight()
	require.Equal(t, 7, buffer.CursorValue())

	buffer.MoveWordRight()
	require.Equal(t, len([]rune("hello\nworld")), buffer.CursorValue())

	buffer.MoveWordLeft()
	require.Equal(t, 6, buffer.CursorValue())

	buffer.MoveLineEnd()
	require.Equal(t, len([]rune("hello\nworld")), buffer.CursorValue())

	buffer.DeleteBackward()
	require.Equal(t, "hello\nworl", buffer.TextValue())

	buffer.MoveLineStart()
	buffer.DeleteForward()
	require.Equal(t, "hello\norl", buffer.TextValue())

	buffer.DeleteToLineEnd()
	require.Equal(t, "hello\n", buffer.TextValue())

	buffer.MoveLeft()
	buffer.DeleteToLineStart()
	require.Equal(t, "\n", buffer.TextValue())

	buffer.SetText("alpha beta")
	buffer.MoveLineStart()
	buffer.DeleteWordForward()
	require.Equal(t, " beta", buffer.TextValue())
}

func TestEditorPrimitivesClampAndHandleBoundaries(t *testing.T) {
	t.Parallel()

	value := []rune("hello\nworld")

	next, cursor := input.InsertRuneAt(value, -10, 'x')
	require.Equal(t, "xhello\nworld", string(next))
	require.Equal(t, 1, cursor)

	next, cursor = input.InsertRuneAt(value, 3, 0)
	require.Equal(t, string(value), string(next))
	require.Equal(t, 3, cursor)

	require.Equal(t, 0, input.MoveCursorLeft(value, -1))
	require.Equal(t, len(value), input.MoveCursorRight(value, 99))
	require.Equal(t, 6, input.MoveCursorLineStart(value, len(value)))
	require.Equal(t, len(value), input.MoveCursorLineEnd(value, 6))
	require.Equal(t, 6, input.MoveCursorWordLeft(value, len(value)))
	require.Equal(t, 5, input.MoveCursorWordRight(value, 0))

	next, cursor = input.DeleteBackwardAt(value, 0)
	require.Equal(t, string(value), string(next))
	require.Equal(t, 0, cursor)

	next, cursor = input.DeleteForwardAt(value, len(value))
	require.Equal(t, string(value), string(next))
	require.Equal(t, len(value), cursor)

	next, cursor = input.DeleteToLineStartAt(value, len(value))
	require.Equal(t, "hello\n", string(next))
	require.Equal(t, 6, cursor)

	next, cursor = input.DeleteToLineEndAt(value, 0)
	require.Equal(t, "\nworld", string(next))
	require.Equal(t, 0, cursor)
}

func TestStringChars(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"h", "é"}, input.StringChars("hé"))
}
