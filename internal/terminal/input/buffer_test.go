package input_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/terminal/input"
)

func TestBufferEditing(t *testing.T) {
	t.Parallel()

	buffer := input.NewBuffer()
	buffer.InsertRune('h')
	buffer.InsertRune('i')
	buffer.MoveLeft()
	buffer.InsertRune('!')

	require.Equal(t, "h!i", buffer.TextValue())
	require.Equal(t, 2, buffer.CursorValue())
	require.Equal(t, []string{"h", "!", "i"}, buffer.Chars)

	buffer.DeleteWordBackward()
	require.Equal(t, "i", buffer.TextValue())
	require.Equal(t, 0, buffer.CursorValue())
}

func TestBufferClearReturnsPreviousText(t *testing.T) {
	t.Parallel()

	buffer := input.NewBuffer()
	buffer.SetText("draft")

	require.Equal(t, "draft", buffer.Clear())
	require.True(t, buffer.Empty())
	require.Empty(t, buffer.Chars)
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

			require.Equal(t, test.want, input.ClampCursor(test.cursor, test.runeCount))
		})
	}
}
