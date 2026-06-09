package input_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/terminal/input"
)

func TestCursorPositionCountsTrailingSpaces(t *testing.T) {
	t.Parallel()

	row, column := input.CursorPosition([]rune("abc   "), 6, 20)
	require.Equal(t, 0, row)
	require.Equal(t, 6, column)
}

func TestBodyLinesPreserveTrailingSpaces(t *testing.T) {
	t.Parallel()

	lines := input.BodyLines([]rune("abc   "), 20)
	require.Equal(t, []string{"abc   "}, lines)
}

func TestCursorPositionUsesCellWidth(t *testing.T) {
	t.Parallel()

	_, column := input.CursorPosition([]rune("語 "), 2, 20)
	require.Equal(t, 3, column)
}

func TestWordDeletion(t *testing.T) {
	t.Parallel()

	value := []rune("hello  world")
	got, cursor := input.DeleteWordBackwardAt(value, len(value))
	require.Equal(t, "hello  ", string(got))
	require.Equal(t, 7, cursor)

	got, cursor = input.DeleteWordForwardAt([]rune("  hello world"), 0)
	require.Equal(t, " world", string(got))
	require.Equal(t, 0, cursor)
}

func TestVisibleLinesKeepsCursorVisible(t *testing.T) {
	t.Parallel()

	visible, skipped := input.VisibleLines([]string{"a", "b", "c", "d"}, 2, 3)
	require.Equal(t, []string{"c", "d"}, visible)
	require.Equal(t, 2, skipped)
}
