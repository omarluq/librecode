package extension

import (
	lua "github.com/yuin/gopher-lua"
)

func (manager *Manager) luaActionAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"run": manager.luaActionRun(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaActionRun(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		checkActiveEvent(state, extensionRuntime).appendAction(ActionCall{Name: name})

		return 0
	}
}
