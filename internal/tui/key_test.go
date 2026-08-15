package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestEventRune(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event *tcell.EventKey
		name  string
		want  rune
	}{
		{name: "nil event", event: nil, want: 0},
		{name: "empty string", event: tcell.NewEventKey(tcell.KeyRune, "", tcell.ModNone), want: 0},
		{name: "ascii rune", event: tcell.NewEventKey(tcell.KeyRune, "a", tcell.ModNone), want: 'a'},
		{name: "multibyte rune", event: tcell.NewEventKey(tcell.KeyRune, "λ", tcell.ModNone), want: 'λ'},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, tui.EventRune(test.event))
		})
	}
}

func TestNewKeyEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event *tcell.EventKey
		want  tui.KeyEvent
	}{
		{
			name:  "rune",
			event: tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone),
			want:  tui.KeyEvent{Key: "x", Text: "x", Ctrl: false, Alt: false, Shift: false},
		},
		{
			name:  "control rune",
			event: tcell.NewEventKey(tcell.KeyRune, "R", tcell.ModCtrl),
			want:  tui.KeyEvent{Key: "ctrl+r", Text: "", Ctrl: true, Alt: false, Shift: false},
		},
		{
			name:  "backtab",
			event: tcell.NewEventKey(tcell.KeyBacktab, "", tcell.ModShift),
			want:  tui.KeyEvent{Key: "shift+tab", Text: "", Ctrl: false, Alt: false, Shift: true},
		},
		{
			name:  "control tab",
			event: tcell.NewEventKey(tcell.KeyTab, "", tcell.ModCtrl),
			want:  tui.KeyEvent{Key: "tab", Text: "", Ctrl: true, Alt: false, Shift: false},
		},
		{
			name:  "alt tab",
			event: tcell.NewEventKey(tcell.KeyTab, "", tcell.ModAlt),
			want:  tui.KeyEvent{Key: "tab", Text: "", Ctrl: false, Alt: true, Shift: false},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tui.NewKeyEvent(test.event)
			require.True(t, ok)
			require.Equal(t, test.want, got)
		})
	}
}

func TestNewKeyEventUnknown(t *testing.T) {
	t.Parallel()

	_, ok := tui.NewKeyEvent(tcell.NewEventKey(tcell.KeyF1, "", tcell.ModNone))
	require.False(t, ok)
}
