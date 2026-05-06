package extension

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

func mapToLuaTable(state *lua.LState, values map[string]any) *lua.LTable {
	table := state.NewTable()
	for key, value := range values {
		state.SetField(table, key, goValueToLua(state, value))
	}

	return table
}

func goValueToLua(state *lua.LState, value any) lua.LValue {
	switch typedValue := value.(type) {
	case nil:
		return lua.LNil
	case string:
		return lua.LString(typedValue)
	case bool:
		return lua.LBool(typedValue)
	case int:
		return lua.LNumber(typedValue)
	case int64:
		return lua.LNumber(typedValue)
	case float64:
		return lua.LNumber(typedValue)
	case map[string]any:
		return mapToLuaTable(state, typedValue)
	case []any:
		return sliceToLuaTable(state, typedValue)
	default:
		return lua.LString(fmt.Sprint(typedValue))
	}
}

func sliceToLuaTable(state *lua.LState, values []any) *lua.LTable {
	table := state.NewTable()
	for valueIndex, value := range values {
		state.RawSetInt(table, valueIndex+1, goValueToLua(state, value))
	}

	return table
}

func luaValueToGo(value lua.LValue) any {
	switch typedValue := value.(type) {
	case lua.LBool:
		return bool(typedValue)
	case lua.LNumber:
		return float64(typedValue)
	case lua.LString:
		return string(typedValue)
	case *lua.LTable:
		return luaTableToMap(typedValue)
	default:
		return nil
	}
}

func luaTableToMap(table *lua.LTable) map[string]any {
	values := map[string]any{}
	table.ForEach(func(key lua.LValue, value lua.LValue) {
		values[key.String()] = luaValueToGo(value)
	})

	return values
}

func luaToolResult(value lua.LValue) ToolResult {
	if table, ok := value.(*lua.LTable); ok {
		contentValue := table.RawGetString("content")
		detailsValue := table.RawGetString("details")
		return ToolResult{
			Content: contentValue.String(),
			Details: luaDetails(detailsValue),
		}
	}

	return ToolResult{
		Content: value.String(),
		Details: map[string]any{},
	}
}

func luaDetails(value lua.LValue) map[string]any {
	if table, ok := value.(*lua.LTable); ok {
		return luaTableToMap(table)
	}

	return map[string]any{}
}
