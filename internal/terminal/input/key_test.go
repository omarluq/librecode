package input_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/terminal/input"
)

func TestComposerKeyEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event *tcell.EventKey
		want  string
		text  string
		ctrl  bool
	}{
		{
			name:  "rune",
			event: tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone),
			want:  "x",
			text:  "x",
			ctrl:  false,
		},
		{
			name:  "control rune",
			event: tcell.NewEventKey(tcell.KeyRune, "R", tcell.ModCtrl),
			want:  "ctrl+r",
			text:  "",
			ctrl:  true,
		},
		{
			name:  "backtab",
			event: tcell.NewEventKey(tcell.KeyBacktab, "", tcell.ModShift),
			want:  "shift+tab",
			text:  "",
			ctrl:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := input.ComposerKeyEvent(test.event)
			require.True(t, ok)
			require.Equal(t, test.want, got.Key)
			require.Equal(t, test.text, got.Text)
			require.Equal(t, test.ctrl, got.Ctrl)
		})
	}
}

func TestComposerKeyEventUnknown(t *testing.T) {
	t.Parallel()

	_, ok := input.ComposerKeyEvent(tcell.NewEventKey(tcell.KeyF1, "", tcell.ModNone))
	require.False(t, ok)
}
