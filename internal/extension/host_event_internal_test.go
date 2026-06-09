package extension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLuaHostEventSnapshotCopiesContextAndData(t *testing.T) {
	t.Parallel()

	event := newLuaHostEvent(&TerminalEvent{
		Buffers: map[string]BufferState{
			luaBufferComposer: newBufferState(luaBufferComposer, "hello"),
		},
		Windows: map[string]WindowState{
			luaBufferComposer: testLuaHostWindow(),
		},
		Layout:  LayoutState{Windows: map[string]WindowState{}, Width: 10, Height: 1},
		Context: map[string]any{"mode": "chat"},
		Data:    map[string]any{"count": 1},
		Name:    "key",
		Key:     ComposerKeyEvent{Key: "x", Text: "x", Alt: false, Ctrl: false, Shift: false},
		Focus: FocusState{
			Kind:      "",
			Window:    luaBufferComposer,
			Buffer:    luaBufferComposer,
			Role:      "",
			PanelKind: "",
			Exclusive: false,
		},
	})

	snapshot := event.eventSnapshot()
	snapshot.Context["mode"] = "mutated"
	snapshot.Data["count"] = 2

	require.NotNil(t, snapshot)
	assert.Equal(t, "key", snapshot.Name)
	assert.Equal(t, "chat", event.context["mode"])
	assert.Equal(t, 1, event.data["count"])
}

func testLuaHostWindow() WindowState {
	return WindowState{
		Metadata:  map[string]any{},
		Name:      luaBufferComposer,
		Role:      luaBufferComposer,
		Buffer:    luaBufferComposer,
		Renderer:  "",
		X:         0,
		Y:         0,
		Width:     10,
		Height:    1,
		CursorRow: 0,
		CursorCol: 0,
		Visible:   true,
	}
}
