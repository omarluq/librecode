package extension_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

const (
	testFocusAutocomplete = "autocomplete"
	testFocusModel        = "model"
	testFocusTranscript   = "transcript"
)

type focusedKeymapCase struct {
	name         string
	wantText     string
	focus        extension.FocusState
	wantConsumed bool
}

func TestManager_KeymapsUseFocusedTargetsOnly(t *testing.T) {
	t.Parallel()

	manager := loadFocusedKeymapTestExtension(t)

	for _, testCase := range focusedKeymapCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			event := testTerminalEventWithComposerWindow("", "x")
			event.Focus = testCase.focus

			result, err := manager.HandleTerminalEvent(context.Background(), &event)
			require.NoError(t, err)

			assert.Equal(t, testCase.wantConsumed, result.Consumed)
			if testCase.wantConsumed {
				require.Contains(t, result.Buffers, testBufferComposer)
				assert.Equal(t, testCase.wantText, result.Buffers[testBufferComposer].Text)
			} else {
				assert.NotContains(t, result.Buffers, testBufferComposer)
			}
		})
	}
}

func loadFocusedKeymapTestExtension(t *testing.T) *extension.Manager {
	t.Helper()

	return loadTestExtension(t, `
local lc = require("librecode")
lc.keymap.set({ role = "composer" }, "x", function()
  lc.buf.set_text("composer", "role")
  lc.event.consume()
end)
lc.keymap.set({ focus = "autocomplete" }, "x", function()
  lc.buf.set_text("composer", "focus")
  lc.event.consume()
end)
`)
}

func focusedKeymapCases() []focusedKeymapCase {
	return []focusedKeymapCase{
		{
			focus:        testComposerFocus(),
			name:         "composer role target matches composer focus",
			wantText:     "role",
			wantConsumed: true,
		},
		{
			focus: extension.FocusState{
				Kind:      testFocusAutocomplete,
				Window:    testFocusAutocomplete,
				Buffer:    "status",
				Role:      testFocusAutocomplete,
				PanelKind: "",
				Exclusive: true,
			},
			name:         "autocomplete focus target matches autocomplete focus",
			wantText:     "focus",
			wantConsumed: true,
		},
		{
			focus: extension.FocusState{
				Kind:      "panel",
				Window:    testFocusTranscript,
				Buffer:    testFocusTranscript,
				Role:      testFocusModel,
				PanelKind: testFocusModel,
				Exclusive: true,
			},
			name:         "panel focus does not match visible composer role",
			wantText:     "",
			wantConsumed: false,
		},
		{
			focus: extension.FocusState{
				Kind:      testFocusTranscript,
				Window:    testFocusTranscript,
				Buffer:    testFocusTranscript,
				Role:      testFocusTranscript,
				PanelKind: "",
				Exclusive: false,
			},
			name:         "different focused role does not match composer role",
			wantText:     "",
			wantConsumed: false,
		},
	}
}

func TestManager_TerminalEventsExposeFocusToLua(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")
lc.on("key", function(event)
  local fields = {
    event.focus.kind,
    event.focus.window,
    event.focus.buffer,
    event.focus.role,
    event.focus.panel_kind,
    tostring(event.focus.exclusive),
  }
  lc.buf.set_text("seen", table.concat(fields, ":"))
end)
`)
	event := testTerminalEventWithComposerWindow("", "x")
	event.Focus = extension.FocusState{
		Kind:      "panel",
		Window:    testFocusTranscript,
		Buffer:    testFocusTranscript,
		Role:      testFocusModel,
		PanelKind: testFocusModel,
		Exclusive: true,
	}

	result, err := manager.HandleTerminalEvent(context.Background(), &event)
	require.NoError(t, err)

	assert.Equal(t, "panel:transcript:transcript:model:model:true", result.Buffers["seen"].Text)
}
