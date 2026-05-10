package extension

import (
	lua "github.com/yuin/gopher-lua"
	"time"
)

func (manager *Manager) luaTimerAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"defer":    manager.luaTimerDefer(extensionRuntime),
		"interval": manager.luaTimerInterval(extensionRuntime),
		"stop":     manager.luaTimerStop(),
	})

	return apiTable
}

func (manager *Manager) luaTimerDefer(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		delay := luaDurationMillis(state.CheckNumber(1))
		function := state.CheckFunction(2)
		id := manager.registerTimer(extensionRuntime, delay, 0, function)
		state.Push(lua.LNumber(id))

		return 1
	}
}

func (manager *Manager) luaTimerInterval(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		interval := luaDurationMillis(state.CheckNumber(1))
		function := state.CheckFunction(2)
		id := manager.registerTimer(extensionRuntime, interval, interval, function)
		state.Push(lua.LNumber(id))

		return 1
	}
}

func (manager *Manager) luaTimerStop() lua.LGFunction {
	return func(state *lua.LState) int {
		id := uint64(state.CheckNumber(1))
		manager.cancelTimer(id)

		return 0
	}
}

func luaDurationMillis(value lua.LNumber) time.Duration {
	millis := float64(value)
	if millis < 0 {
		millis = 0
	}

	return time.Duration(millis * float64(time.Millisecond))
}
