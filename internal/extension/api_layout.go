package extension

import (
	lua "github.com/yuin/gopher-lua"
)

func (manager *Manager) luaLayoutAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		luaFieldGet: manager.luaLayoutGet(extensionRuntime),
		luaFieldSet: manager.luaLayoutSet(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaLayoutGet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		hostEvent := checkActiveEvent(state, extensionRuntime)
		state.Push(mapToLuaTable(state, layoutForLua(&hostEvent.layout)))

		return 1
	}
}

func (manager *Manager) luaLayoutSet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		layout := luaLayoutState(state.CheckAny(1))
		if layout == nil {
			state.RaiseError("layout.set expects a layout table")
			return 0
		}
		checkActiveEvent(state, extensionRuntime).setLayout(layout)

		return 0
	}
}
