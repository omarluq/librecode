package extension

import (
	lua "github.com/yuin/gopher-lua"
)

func checkActiveEvent(state *lua.LState, extensionRuntime *luaExtension) *luaHostEvent {
	if extensionRuntime.activeEvent == nil {
		state.RaiseError("librecode runtime buffer API called outside an event")
	}

	return extensionRuntime.activeEvent
}
