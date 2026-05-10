package extension

import (
	lua "github.com/yuin/gopher-lua"
)

func (manager *Manager) luaEventAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"consume": manager.luaEventConsume(extensionRuntime),
		"stop":    manager.luaEventStop(extensionRuntime),
	})

	return apiTable
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
