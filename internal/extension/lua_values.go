package extension

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

const luaFieldText = "text"

func mapToLuaTable(state *lua.LState, values map[string]any) *lua.LTable {
	table := state.NewTable()
	for key, value := range values {
		state.SetField(table, key, goValueToLua(state, value))
	}

	return table
}

func goValueToLua(state *lua.LState, value any) lua.LValue {
	if luaValue, ok := scalarGoValueToLua(value); ok {
		return luaValue
	}

	switch typedValue := value.(type) {
	case map[string]any:
		return mapToLuaTable(state, typedValue)
	case []any:
		return sliceToLuaTable(state, typedValue)
	case []string:
		return stringSliceToLuaTable(state, typedValue)
	default:
		return lua.LString(fmt.Sprint(typedValue))
	}
}

func scalarGoValueToLua(value any) (lua.LValue, bool) {
	switch typedValue := value.(type) {
	case nil:
		return lua.LNil, true
	case string:
		return lua.LString(typedValue), true
	case bool:
		return lua.LBool(typedValue), true
	case int:
		return lua.LNumber(typedValue), true
	case int64:
		return lua.LNumber(typedValue), true
	case float64:
		return lua.LNumber(typedValue), true
	default:
		return lua.LNil, false
	}
}

func sliceToLuaTable(state *lua.LState, values []any) *lua.LTable {
	table := state.NewTable()
	for valueIndex, value := range values {
		state.RawSetInt(table, valueIndex+1, goValueToLua(state, value))
	}

	return table
}

func stringSliceToLuaTable(state *lua.LState, values []string) *lua.LTable {
	table := state.NewTable()
	for valueIndex, value := range values {
		state.RawSetInt(table, valueIndex+1, lua.LString(value))
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

func luaComposerResult(value lua.LValue) ComposerResult {
	table, ok := value.(*lua.LTable)
	if !ok {
		return emptyComposerResult()
	}

	text, hasText := luaComposerText(table)
	cursor, hasCursor := luaTableInt(table, "cursor", 0)

	return ComposerResult{
		Text:      text,
		Label:     luaTableString(table, "label", ""),
		Cursor:    cursor,
		Handled:   luaTableBool(table, "handled"),
		HasText:   hasText,
		HasCursor: hasCursor,
	}
}

func luaComposerText(table *lua.LTable) (string, bool) {
	charsValue := table.RawGetString("chars")
	if chars, ok := charsValue.(*lua.LTable); ok {
		return strings.Join(luaStringSlice(chars), ""), true
	}

	textValue := table.RawGetString(luaFieldText)
	if textValue == lua.LNil {
		return "", false
	}

	return textValue.String(), true
}

func luaStringSlice(table *lua.LTable) []string {
	values := make([]string, 0, table.Len())
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		values = append(values, table.RawGetInt(valueIndex).String())
	}

	return values
}

func emptyComposerResult() ComposerResult {
	var result ComposerResult

	return result
}

func luaDetails(value lua.LValue) map[string]any {
	if table, ok := value.(*lua.LTable); ok {
		return luaTableToMap(table)
	}

	return map[string]any{}
}

func luaTableBool(table *lua.LTable, key string) bool {
	value := table.RawGetString(key)
	if boolValue, ok := value.(lua.LBool); ok {
		return bool(boolValue)
	}

	return false
}

func luaTableString(table *lua.LTable, key, fallback string) string {
	value := table.RawGetString(key)
	if value == lua.LNil {
		return fallback
	}

	return value.String()
}

func luaTableInt(table *lua.LTable, key string, fallback int) (int, bool) {
	value := table.RawGetString(key)
	if value == lua.LNil {
		return fallback, false
	}
	if numberValue, ok := value.(lua.LNumber); ok {
		return int(numberValue), true
	}

	return fallback, false
}

func luaTableFunction(table *lua.LTable, key string) *lua.LFunction {
	value := table.RawGetString(key)
	function, ok := value.(*lua.LFunction)
	if !ok {
		return nil
	}

	return function
}

func composerEventTable(state *lua.LState, event ComposerKeyEvent) *lua.LTable {
	return mapToLuaTable(state, map[string]any{
		"key":        event.Key,
		luaFieldText: event.Text,
		"ctrl":       event.Ctrl,
		"alt":        event.Alt,
	})
}

func composerStateTable(state *lua.LState, composerState ComposerState) *lua.LTable {
	return mapToLuaTable(state, map[string]any{
		luaFieldText:   composerState.Text,
		"chars":        composerState.Chars,
		"cursor":       composerState.Cursor,
		"working":      composerState.Working,
		"auth_working": composerState.AuthWorking,
	})
}

func terminalEventTable(state *lua.LState, event TerminalEvent) *lua.LTable {
	return mapToLuaTable(state, map[string]any{
		"name":       event.Name,
		"key":        event.Key.Key,
		luaFieldText: event.Key.Text,
		"ctrl":       event.Key.Ctrl,
		"alt":        event.Key.Alt,
		"context":    event.Context,
		"buffers":    bufferMapForLua(event.Buffers),
	})
}

func bufferMapForLua(buffers map[string]BufferState) map[string]any {
	values := make(map[string]any, len(buffers))
	for name, buffer := range buffers {
		values[name] = bufferForLua(&buffer)
	}

	return values
}

func bufferForLua(buffer *BufferState) map[string]any {
	return map[string]any{
		"name":       buffer.Name,
		luaFieldText: buffer.Text,
		"chars":      buffer.Chars,
		"cursor":     buffer.Cursor,
		"label":      buffer.Label,
		"metadata":   buffer.Metadata,
	}
}

func luaBufferState(name string, value lua.LValue) BufferState {
	if table, ok := value.(*lua.LTable); ok {
		text, hasText := luaComposerText(table)
		if !hasText {
			text = ""
		}
		cursor, hasCursor := luaTableInt(table, "cursor", len([]rune(text)))
		if !hasCursor {
			cursor = len([]rune(text))
		}

		return BufferState{
			Metadata: luaDetails(table.RawGetString("metadata")),
			Name:     luaTableString(table, "name", name),
			Text:     text,
			Chars:    stringChars(text),
			Label:    luaTableString(table, "label", ""),
			Cursor:   cursor,
		}
	}

	text := value.String()

	return newBufferState(name, text)
}

func luaBufferAppend(value lua.LValue) BufferAppend {
	if table, ok := value.(*lua.LTable); ok {
		return BufferAppend{
			Name: luaTableString(table, "name", ""),
			Text: luaTableString(table, luaFieldText, ""),
			Role: luaTableString(table, "role", "custom"),
		}
	}

	return BufferAppend{
		Name: "",
		Text: value.String(),
		Role: "custom",
	}
}
