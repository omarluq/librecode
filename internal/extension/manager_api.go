package extension

import lua "github.com/yuin/gopher-lua"

func (manager *Manager) luaBufferAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"get":        manager.luaBufferGet(extensionRuntime),
		"set":        manager.luaBufferSet(extensionRuntime),
		"set_text":   manager.luaBufferSetText(extensionRuntime),
		"append":     manager.luaBufferAppend(extensionRuntime),
		"set_cursor": manager.luaBufferSetCursor(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaEventAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"consume": manager.luaEventConsume(extensionRuntime),
		"stop":    manager.luaEventStop(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaBufferGet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		state.Push(bufferStateTable(state, &buffer))

		return 1
	}
}

func (manager *Manager) luaBufferSet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		value := state.CheckAny(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := luaBufferState(name, value)
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferSetText(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		text := state.CheckString(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Text = text
		buffer.Chars = stringChars(text)
		buffer.Cursor = len([]rune(text))
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferAppend(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		value := state.CheckAny(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		hostEvent.appendBuffer(bufferAppendFromLua(name, value))

		return 0
	}
}

func (manager *Manager) luaBufferSetCursor(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		cursor := state.CheckInt(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Cursor = cursor
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaEventConsume(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		checkActiveEvent(state, extensionRuntime).consumed = true

		return 0
	}
}

func (manager *Manager) luaEventStop(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		hostEvent := checkActiveEvent(state, extensionRuntime)
		hostEvent.consumed = true
		hostEvent.stopped = true

		return 0
	}
}

func checkActiveEvent(state *lua.LState, extensionRuntime *luaExtension) *luaHostEvent {
	if extensionRuntime.activeEvent == nil {
		state.RaiseError("librecode runtime buffer API called outside an event")
	}

	return extensionRuntime.activeEvent
}

func bufferStateTable(state *lua.LState, buffer *BufferState) *lua.LTable {
	return mapToLuaTable(state, bufferForLua(buffer))
}

func bufferAppendFromLua(name string, value lua.LValue) BufferAppend {
	if table, ok := value.(*lua.LTable); ok {
		bufferAppend := luaBufferAppend(table)
		if bufferAppend.Name == "" {
			bufferAppend.Name = name
		}

		return bufferAppend
	}

	return BufferAppend{
		Name: name,
		Text: value.String(),
		Role: "custom",
	}
}
