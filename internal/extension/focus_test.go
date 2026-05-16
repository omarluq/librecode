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
	testFocusTranscript   = "transcript"
)

func TestManager_KeymapsUseFocusedTargetsOnly(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
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

	event := testTerminalEventWithComposerWindow("", "x")
	event.Focus = extension.FocusState{
		Kind:      testFocusAutocomplete,
		Window:    testFocusAutocomplete,
		Buffer:    "status",
		Role:      testFocusAutocomplete,
		PanelKind: "",
		Exclusive: true,
	}

	result, err := manager.HandleTerminalEvent(context.Background(), &event)
	require.NoError(t, err)

	assert.True(t, result.Consumed)
	assert.Equal(t, "focus", result.Buffers[testBufferComposer].Text)
}

func TestManager_TerminalEventsExposeFocusToLua(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")
lc.on("key", function(event)
  lc.buf.set_text("seen", event.focus.kind .. ":" .. event.focus.role .. ":" .. tostring(event.focus.exclusive))
end)
`)
	event := testTerminalEventWithComposerWindow("", "x")
	event.Focus = extension.FocusState{
		Kind:      "panel",
		Window:    testFocusTranscript,
		Buffer:    testFocusTranscript,
		Role:      "model",
		PanelKind: "model",
		Exclusive: true,
	}

	result, err := manager.HandleTerminalEvent(context.Background(), &event)
	require.NoError(t, err)

	assert.Equal(t, "panel:model:true", result.Buffers["seen"].Text)
}
