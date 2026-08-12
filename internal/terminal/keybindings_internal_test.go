package terminal

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReleasedKeyEventIsIgnored(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerBuffer.SetText("draft")

	event := tcell.NewEventKeyEx(tcell.KeyEnter, "", tcell.ModNone, false, tcell.KeyEnter, 0)

	_, err := app.handleEvent(t.Context(), event)
	require.NoError(t, err)
	assert.Equal(t, "draft", app.composerBuffer.TextValue())
	assert.False(t, app.working)
}

func TestInputDeliveryKeybindings(t *testing.T) {
	t.Parallel()

	bindings := newDefaultKeybindings()
	tests := []struct {
		event  *tcell.EventKey
		action actionID
		name   string
	}{
		{
			name: "enhanced shift enter", event: tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModShift),
			action: actionInputNewLine,
		},
		{name: "ctrl j key", event: tcell.NewEventKey(tcell.KeyCtrlJ, "", tcell.ModNone), action: actionInputNewLine},
		{
			name: "ctrl j rune metadata", event: tcell.NewEventKey(tcell.KeyRune, "j", tcell.ModCtrl),
			action: actionInputNewLine,
		},
		{name: "plain enter", event: tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone), action: actionInputSubmit},
		{
			name: "alt enter fallback", event: tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModAlt),
			action: actionMessageFollowUp,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, bindings.matches(testCase.event, testCase.action))
		})
	}
}

func TestNormalizeKeyNameAliasesBacktabToShiftTab(t *testing.T) {
	t.Parallel()

	assert.Equal(t, keyShiftTab, normalizeKeyName("BackTab"))

	for _, event := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyBacktab, "", tcell.ModNone),
		tcell.NewEventKey(tcell.KeyTab, "", tcell.ModShift),
	} {
		keys := normalizedEventKeys(event)
		_, ok := keys[keyShiftTab]
		assert.True(t, ok)
	}
}
