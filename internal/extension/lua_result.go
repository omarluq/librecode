package extension

import lua "github.com/yuin/gopher-lua"

type luaResult struct {
	value lua.LValue
}

func newLuaResult(value lua.LValue) luaResult {
	return luaResult{value: value}
}

func (result luaResult) String() string {
	return result.value.String()
}

func (result luaResult) IsTrue() bool {
	return result.value == lua.LTrue
}

func (result luaResult) ToolResult() ToolResult {
	return luaToolResult(result.value)
}

func (result luaResult) ApplyTo(event *luaHostEvent) {
	event.applyLuaResult(result.value)
}

func (result luaResult) ApplyLifecycleTo(dispatchResult *LifecycleDispatchResult) {
	applyLifecycleLuaResult(dispatchResult, result.value)
}
