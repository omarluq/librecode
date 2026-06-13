package extension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

const luaTestMainWindow = "main"

func TestLuaHostEventApplyLuaResultMutatesChangedState(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	event := newLuaHostEvent(&TerminalEvent{
		Buffers: map[string]BufferState{
			luaBufferComposer: newBufferState(luaBufferComposer, "old"),
		},
		Windows: map[string]WindowState{
			luaBufferComposer: testLuaHostWindow(),
		},
		Layout: testLuaHostLayout(map[string]WindowState{
			luaBufferComposer: testLuaHostWindow(),
		}),
		Context: map[string]any{},
		Data:    map[string]any{},
		Name:    "key",
		Key:     testComposerKeyEvent(),
		Focus:   testFocusState(),
	})

	event.applyLuaResult(luaHostEventResultFixture(state))
	output := event.result()

	assert.True(t, event.consumed)
	assert.True(t, event.stopped)
	require.Contains(t, output.Buffers, luaBufferComposer)
	assert.Equal(t, "new text", output.Buffers[luaBufferComposer].Text)
	require.Contains(t, output.Windows, luaTestMainWindow)
	assert.Equal(t, luaBufferComposer, output.Windows[luaTestMainWindow].Buffer)
	require.NotNil(t, output.Layout)
	assert.Equal(t, 80, output.Layout.Width)
	assert.Len(t, output.Actions, 1)
	assert.Equal(t, "accept", output.Actions[0].Name)
	assert.Len(t, output.UIDrawOps, 1)
	assert.Equal(t, []string{luaTestMainWindow}, output.ResetUIWindows)
	assert.Equal(t, []string{"old-buffer"}, output.DeletedBuffers)
	assert.Equal(t, []string{"old-window"}, output.DeletedWindows)
	require.NotNil(t, output.UICursor)
	assert.Equal(t, 2, output.UICursor.Col)
}

func TestLuaHostEventDeleteThenSetRemovesDeletedMarkers(t *testing.T) {
	t.Parallel()

	event := newLuaHostEvent(&TerminalEvent{
		Buffers: map[string]BufferState{"b": newBufferState("b", "old")},
		Windows: map[string]WindowState{"w": testLuaHostWindow()},
		Layout:  testLuaHostLayout(map[string]WindowState{"w": testLuaHostWindow()}),
		Context: map[string]any{},
		Data:    map[string]any{},
		Name:    "",
		Key:     testComposerKeyEvent(),
		Focus:   testFocusState(),
	})

	event.deleteBuffer("b")

	buffer := newBufferState("b", "new")
	event.setBuffer("b", &buffer)
	event.deleteWindow("w")

	window := testLuaHostWindow()
	event.setWindow("w", &window)

	output := event.result()
	assert.Empty(t, output.DeletedBuffers)
	assert.Empty(t, output.DeletedWindows)
	assert.Contains(t, output.Buffers, "b")
	assert.Contains(t, output.Windows, "w")
}

func TestLuaHostEventNamesAndFallbacks(t *testing.T) {
	t.Parallel()

	event := newLuaHostEvent(&TerminalEvent{
		Buffers: map[string]BufferState{"z": newBufferState("z", ""), "a": newBufferState("a", "")},
		Windows: map[string]WindowState{"z": testLuaHostWindow(), "a": testLuaHostWindow()},
		Layout:  testLuaHostLayout(map[string]WindowState{}),
		Context: map[string]any{},
		Data:    map[string]any{},
		Name:    "",
		Key:     testComposerKeyEvent(),
		Focus:   testFocusState(),
	})

	assert.Equal(t, []string{"a", "z"}, event.bufferNames())
	assert.Equal(t, []string{"a", "z"}, event.windowNames())
	missingBuffer := event.buffer("missing")
	assert.Equal(t, "missing", missingBuffer.Name)

	missingWindow, ok := event.window("missing")
	assert.False(t, ok)
	assert.Empty(t, missingWindow.Name)
	assert.Nil(t, cloneLayoutPtr(nil))
}

func luaHostEventResultFixture(state *lua.LState) *lua.LTable {
	result := state.NewTable()
	state.SetField(result, "handled", lua.LBool(true))
	state.SetField(result, "stop", lua.LBool(true))
	state.SetField(result, luaFieldBuffers, luaTable(state, map[string]lua.LValue{
		luaBufferComposer: luaTable(state, map[string]lua.LValue{luaFieldText: lua.LString("new text")}),
	}))
	state.SetField(result, luaFieldWindows, luaTable(state, map[string]lua.LValue{
		luaTestMainWindow: luaTable(state, map[string]lua.LValue{
			keymapScopeBuffer: lua.LString(luaBufferComposer),
			luaFieldWidth:     lua.LNumber(20),
		}),
	}))
	state.SetField(result, luaFieldActions, luaActionsFixture(state))
	state.SetField(result, luaFieldUIDrawOps, luaDrawOpsFixture(state))
	state.SetField(result, luaFieldResetUIWindows, luaResetWindowsFixture(state))
	state.SetField(result, luaFieldUICursor, luaCursorTable(state))
	state.SetField(result, luaFieldDeletedBuffers, repeatedLuaStrings(state, "old-buffer"))
	state.SetField(result, luaFieldDeletedWindows, repeatedLuaStrings(state, "old-window"))
	state.SetField(result, luaFieldLayout, luaLayoutTable(state))

	return result
}

func luaActionsFixture(state *lua.LState) *lua.LTable {
	return luaArray(state, []lua.LValue{
		luaTable(state, map[string]lua.LValue{luaFieldName: lua.LString("accept")}),
		luaTable(state, map[string]lua.LValue{luaFieldName: lua.LString("")}),
	})
}

func luaDrawOpsFixture(state *lua.LState) *lua.LTable {
	return luaArray(state, []lua.LValue{
		luaDrawOpTable(state),
		luaTable(state, map[string]lua.LValue{luaFieldKind: lua.LString("ignored")}),
	})
}

func luaDrawOpTable(state *lua.LState) *lua.LTable {
	return luaTable(state, map[string]lua.LValue{
		keymapScopeWindow: lua.LString(luaTestMainWindow),
		luaFieldKind:      lua.LString(UIDrawKindText),
		luaFieldText:      lua.LString("hi"),
	})
}

func luaResetWindowsFixture(state *lua.LState) *lua.LTable {
	return luaArray(state, []lua.LValue{
		lua.LString(luaTestMainWindow),
		lua.LString(luaTestMainWindow),
		lua.LString(""),
	})
}

func luaCursorTable(state *lua.LState) *lua.LTable {
	return luaTable(state, map[string]lua.LValue{
		keymapScopeWindow: lua.LString(luaTestMainWindow),
		"row":             lua.LNumber(1),
		"col":             lua.LNumber(2),
	})
}

func luaLayoutTable(state *lua.LState) *lua.LTable {
	return luaTable(state, map[string]lua.LValue{
		luaFieldWidth:  lua.LNumber(80),
		luaFieldHeight: lua.LNumber(24),
		luaFieldWindows: luaTable(state, map[string]lua.LValue{
			luaTestMainWindow: luaTable(state, map[string]lua.LValue{
				keymapScopeBuffer: lua.LString(luaBufferComposer),
			}),
		}),
	})
}

func repeatedLuaStrings(state *lua.LState, value string) *lua.LTable {
	return luaArray(state, []lua.LValue{lua.LString(value), lua.LString(value)})
}

func testLuaHostLayout(windows map[string]WindowState) LayoutState {
	return LayoutState{Windows: windows, Width: 10, Height: 1}
}

func testComposerKeyEvent() ComposerKeyEvent {
	return ComposerKeyEvent{Key: "", Text: "", Ctrl: false, Alt: false, Shift: false}
}

func testFocusState() FocusState {
	return FocusState{Kind: "", Window: "", Buffer: "", Role: "", PanelKind: "", Exclusive: false}
}

func luaTable(state *lua.LState, values map[string]lua.LValue) *lua.LTable {
	table := state.NewTable()
	for key, value := range values {
		state.SetField(table, key, value)
	}

	return table
}

func luaArray(state *lua.LState, values []lua.LValue) *lua.LTable {
	table := state.NewTable()
	for index, value := range values {
		state.RawSetInt(table, index+1, value)
	}

	return table
}
