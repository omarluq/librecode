package keyevent_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/omarluq/librecode/internal/terminal/keyevent"
	"github.com/stretchr/testify/assert"
)

func TestRune(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event *tcell.EventKey
		name  string
		want  rune
	}{
		{
			name:  "nil event",
			event: nil,
			want:  0,
		},
		{
			name:  "empty string",
			event: tcell.NewEventKey(tcell.KeyRune, "", tcell.ModNone),
			want:  0,
		},
		{
			name:  "ascii rune",
			event: tcell.NewEventKey(tcell.KeyRune, "a", tcell.ModNone),
			want:  'a',
		},
		{
			name:  "multibyte rune",
			event: tcell.NewEventKey(tcell.KeyRune, "λ", tcell.ModNone),
			want:  'λ',
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, keyevent.Rune(testCase.event))
		})
	}
}
