package extension

import (
	lua "github.com/yuin/gopher-lua"
)

func (manager *Manager) luaWindowAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		luaFieldCreate: manager.luaWindowCreate(extensionRuntime),
		"delete":       manager.luaWindowDelete(extensionRuntime),
		"find":         manager.luaWindowFind(extensionRuntime),
		luaFieldGet:    manager.luaWindowGet(extensionRuntime),
		"get_buf":      manager.luaWindowGetBuffer(extensionRuntime),
		"get_buffer":   manager.luaWindowGetBuffer(extensionRuntime),
		"get_var":      manager.luaWindowGetVar(extensionRuntime),
		"list":         manager.luaWindowList(extensionRuntime),
		luaFieldSet:    manager.luaWindowSet(extensionRuntime),
		"set_buf":      manager.luaWindowSetBuffer(extensionRuntime),
		"set_buffer":   manager.luaWindowSetBuffer(extensionRuntime),
		"set_renderer": manager.luaWindowSetRenderer(extensionRuntime),
		"set_var":      manager.luaWindowSetVar(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaWindowList(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		hostEvent := checkActiveEvent(state, extensionRuntime)
		state.Push(stringSliceToLuaTable(state, hostEvent.windowNames()))

		return 1
	}
}

func (manager *Manager) luaWindowCreate(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		value := state.Get(2)
		window := WindowState{
			Metadata:  map[string]any{},
			Name:      name,
			Role:      "",
			Buffer:    "",
			Renderer:  "",
			X:         0,
			Y:         0,
			Width:     0,
			Height:    0,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		}
		if value != lua.LNil {
			window = luaWindowState(name, value)
		}
		checkActiveEvent(state, extensionRuntime).setWindow(name, &window)
		state.Push(mapToLuaTable(state, windowForLua(&window)))

		return 1
	}
}

func (manager *Manager) luaWindowDelete(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		checkActiveEvent(state, extensionRuntime).deleteWindow(name)

		return 0
	}
}

func (manager *Manager) luaWindowGet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		window, ok := checkActiveEvent(state, extensionRuntime).window(name)
		if !ok {
			state.Push(lua.LNil)
			return 1
		}
		state.Push(mapToLuaTable(state, windowForLua(&window)))

		return 1
	}
}

func (manager *Manager) luaWindowSet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		window := luaWindowState(name, state.CheckAny(2))
		checkActiveEvent(state, extensionRuntime).setWindow(name, &window)

		return 0
	}
}

func (manager *Manager) luaWindowFind(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		opts := state.CheckTable(1)
		name := luaTableString(opts, "name", "")
		role := luaTableString(opts, "role", "")
		buffer := luaTableString(opts, "buffer", "")
		windows := checkActiveEvent(state, extensionRuntime).windows
		for windowName := range windows {
			window := windows[windowName]
			if name != "" && windowName != name {
				continue
			}
			if role != "" && window.Role != role {
				continue
			}
			if buffer != "" && window.Buffer != buffer {
				continue
			}
			state.Push(lua.LString(windowName))
			return 1
		}
		state.Push(lua.LNil)

		return 1
	}
}

func (manager *Manager) luaWindowGetBuffer(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		window, ok := checkActiveEvent(state, extensionRuntime).window(name)
		if !ok {
			state.Push(lua.LNil)
			return 1
		}
		state.Push(lua.LString(window.Buffer))

		return 1
	}
}

func (manager *Manager) luaWindowSetBuffer(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		bufferName := state.CheckString(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		window := hostEventWindow(hostEvent, name)
		window.Buffer = bufferName
		hostEvent.setWindow(name, &window)

		return 0
	}
}

func (manager *Manager) luaWindowSetRenderer(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		renderer := state.CheckString(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		window := hostEventWindow(hostEvent, name)
		window.Renderer = renderer
		hostEvent.setWindow(name, &window)

		return 0
	}
}

func (manager *Manager) luaWindowGetVar(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		key := state.CheckString(2)
		window, ok := checkActiveEvent(state, extensionRuntime).window(name)
		if !ok {
			state.Push(lua.LNil)
			return 1
		}
		state.Push(goValueToLua(state, window.Metadata[key]))

		return 1
	}
}

func (manager *Manager) luaWindowSetVar(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		key := state.CheckString(2)
		value := state.CheckAny(3)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		window := hostEventWindow(hostEvent, name)
		if window.Metadata == nil {
			window.Metadata = map[string]any{}
		}
		window.Metadata[key] = luaValueToGo(value)
		hostEvent.setWindow(name, &window)

		return 0
	}
}

func hostEventWindow(hostEvent *luaHostEvent, name string) WindowState {
	window, ok := hostEvent.window(name)
	if ok {
		return window
	}

	return WindowState{
		Metadata:  map[string]any{},
		Name:      name,
		Role:      "",
		Buffer:    "",
		Renderer:  "",
		X:         0,
		Y:         0,
		Width:     0,
		Height:    0,
		CursorRow: 0,
		CursorCol: 0,
		Visible:   true,
	}
}
