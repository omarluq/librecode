package extension

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

const (
	luaBufferComposer = "composer"
	luaFieldCreate    = "create"
	luaFieldGet       = "get"
	luaFieldKey       = "key"
	luaFieldMetadata  = "metadata"
	luaFieldName      = "name"
	luaFieldSet       = "set"
	luaFieldText      = "text"
)

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

func luaStringSlice(table *lua.LTable) []string {
	values := make([]string, 0, table.Len())
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		values = append(values, table.RawGetInt(valueIndex).String())
	}

	return values
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

func terminalEventTable(state *lua.LState, event *TerminalEvent) *lua.LTable {
	composer := event.Buffers[luaBufferComposer]

	return mapToLuaTable(state, map[string]any{
		luaFieldName:   event.Name,
		luaFieldKey:    event.Key.Key,
		luaFieldText:   event.Key.Text,
		"ctrl":         event.Key.Ctrl,
		"alt":          event.Key.Alt,
		"shift":        event.Key.Shift,
		"context":      event.Context,
		"data":         event.Data,
		"buffers":      bufferMapForLua(event.Buffers),
		"windows":      windowMapForLua(event.Windows),
		"layout":       layoutForLua(&event.Layout),
		"composer":     bufferForLua(&composer),
		"working":      luaContextBool(event.Context, "working"),
		"auth_working": luaContextBool(event.Context, "auth_working"),
	})
}

func luaContextBool(eventContext map[string]any, key string) bool {
	value, ok := eventContext[key].(bool)
	if !ok {
		return false
	}

	return value
}

func bufferMapForLua(buffers map[string]BufferState) map[string]any {
	values := make(map[string]any, len(buffers))
	for name, buffer := range buffers {
		values[name] = bufferForLua(&buffer)
	}

	return values
}

func windowMapForLua(windows map[string]WindowState) map[string]any {
	values := make(map[string]any, len(windows))
	for name := range windows {
		window := windows[name]
		values[name] = windowForLua(&window)
	}

	return values
}

func layoutForLua(layout *LayoutState) map[string]any {
	return map[string]any{
		"windows": windowMapForLua(layout.Windows),
		"width":   layout.Width,
		"height":  layout.Height,
	}
}

func bufferForLua(buffer *BufferState) map[string]any {
	return map[string]any{
		luaFieldName:     buffer.Name,
		luaFieldText:     buffer.Text,
		"blocks":         bufferBlocksForLua(buffer.Blocks),
		"chars":          buffer.Chars,
		"cursor":         buffer.Cursor,
		"label":          buffer.Label,
		luaFieldMetadata: buffer.Metadata,
	}
}

func windowForLua(window *WindowState) map[string]any {
	return map[string]any{
		luaFieldName:     window.Name,
		"role":           window.Role,
		"buffer":         window.Buffer,
		"renderer":       window.Renderer,
		"x":              window.X,
		"y":              window.Y,
		"width":          window.Width,
		"height":         window.Height,
		"cursor_row":     window.CursorRow,
		"cursor_col":     window.CursorCol,
		"visible":        window.Visible,
		luaFieldMetadata: window.Metadata,
	}
}

func bufferBlocksForLua(blocks []BufferBlock) []any {
	values := make([]any, 0, len(blocks))
	for index := range blocks {
		values = append(values, bufferBlockForLua(&blocks[index]))
	}

	return values
}

func bufferBlocksTable(state *lua.LState, blocks []BufferBlock) *lua.LTable {
	return sliceToLuaTable(state, bufferBlocksForLua(blocks))
}

func bufferBlockForLua(block *BufferBlock) map[string]any {
	return map[string]any{
		luaFieldMetadata: block.Metadata,
		"created_at":     block.CreatedAt,
		"id":             block.ID,
		"kind":           block.Kind,
		"role":           block.Role,
		"text":           block.Text,
		"index":          block.Index,
		"streaming":      block.Streaming,
	}
}

func luaBufferState(name string, value lua.LValue) BufferState {
	if table, ok := value.(*lua.LTable); ok {
		text, hasText := luaBufferText(table)
		if !hasText {
			text = ""
		}
		cursor, hasCursor := luaTableInt(table, "cursor", len([]rune(text)))
		if !hasCursor {
			cursor = len([]rune(text))
		}

		return BufferState{
			Metadata: luaDetails(table.RawGetString(luaFieldMetadata)),
			Blocks:   luaBufferBlocksFromValue(table.RawGetString("blocks")),
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

func luaBufferText(table *lua.LTable) (string, bool) {
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

func luaBufferBlocks(table *lua.LTable) []BufferBlock {
	blocks := make([]BufferBlock, 0, table.Len())
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		if blockTable, ok := table.RawGetInt(valueIndex).(*lua.LTable); ok {
			blocks = append(blocks, luaBufferBlock(blockTable, len(blocks)))
		}
	}

	return blocks
}

func luaBufferBlocksFromValue(value lua.LValue) []BufferBlock {
	if table, ok := value.(*lua.LTable); ok {
		return luaBufferBlocks(table)
	}

	return []BufferBlock{}
}

func luaBufferBlock(table *lua.LTable, index int) BufferBlock {
	return BufferBlock{
		Metadata:  luaDetails(table.RawGetString(luaFieldMetadata)),
		CreatedAt: luaTableString(table, "created_at", ""),
		ID:        luaTableString(table, "id", ""),
		Kind:      luaTableString(table, "kind", ""),
		Role:      luaTableString(table, "role", ""),
		Text:      luaTableString(table, luaFieldText, ""),
		Index:     luaTableIntValueWithDefault(table, "index", index),
		Streaming: luaTableBool(table, "streaming"),
	}
}

func isBufferBlockTable(table *lua.LTable) bool {
	return table.RawGetString("kind") != lua.LNil ||
		table.RawGetString("role") != lua.LNil ||
		table.RawGetString("id") != lua.LNil ||
		table.RawGetString("created_at") != lua.LNil ||
		table.RawGetString("streaming") != lua.LNil
}

func luaWindowState(name string, value lua.LValue) WindowState {
	if table, ok := value.(*lua.LTable); ok {
		visible, hasVisible := luaTableBoolValue(table, "visible")
		if !hasVisible {
			visible = true
		}

		return WindowState{
			Metadata:  luaDetails(table.RawGetString(luaFieldMetadata)),
			Name:      luaTableString(table, "name", name),
			Role:      luaTableString(table, "role", ""),
			Buffer:    luaTableString(table, "buffer", ""),
			Renderer:  luaTableString(table, "renderer", ""),
			X:         luaTableIntValue(table, "x"),
			Y:         luaTableIntValue(table, "y"),
			Width:     luaTableIntValue(table, "width"),
			Height:    luaTableIntValue(table, "height"),
			CursorRow: luaTableIntValue(table, "cursor_row"),
			CursorCol: luaTableIntValue(table, "cursor_col"),
			Visible:   visible,
		}
	}

	return WindowState{
		Metadata:  map[string]any{},
		Name:      name,
		Role:      "",
		Buffer:    value.String(),
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

func luaUIDrawOp(value lua.LValue) *UIDrawOp {
	if table, ok := value.(*lua.LTable); ok {
		return &UIDrawOp{
			Style: UIStyle{
				FG:     luaTableString(table, "fg", ""),
				BG:     luaTableString(table, "bg", ""),
				Bold:   luaTableBool(table, "bold"),
				Italic: luaTableBool(table, "italic"),
			},
			Window: luaTableString(table, "window", ""),
			Text:   luaTableString(table, "text", ""),
			Row:    luaTableIntValue(table, "row"),
			Col:    luaTableIntValue(table, "col"),
			Clear:  luaTableBool(table, "clear"),
		}
	}

	return nil
}

func luaUICursor(value lua.LValue) *UICursor {
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil
	}

	return &UICursor{
		Window: luaTableString(table, "window", ""),
		Row:    luaTableIntValue(table, "row"),
		Col:    luaTableIntValue(table, "col"),
	}
}

func luaLayoutState(value lua.LValue) *LayoutState {
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil
	}
	windows := map[string]WindowState{}
	windowsValue := table.RawGetString("windows")
	if windowsTable, ok := windowsValue.(*lua.LTable); ok {
		windowsTable.ForEach(func(key lua.LValue, windowValue lua.LValue) {
			name := key.String()
			windows[name] = luaWindowState(name, windowValue)
		})
	}

	return &LayoutState{
		Windows: windows,
		Width:   luaTableIntValue(table, "width"),
		Height:  luaTableIntValue(table, "height"),
	}
}

func luaTableIntValue(table *lua.LTable, key string) int {
	return luaTableIntValueWithDefault(table, key, 0)
}

func luaTableIntValueWithDefault(table *lua.LTable, key string, fallback int) int {
	value, _ := luaTableInt(table, key, fallback)

	return value
}

func luaTableBoolValue(table *lua.LTable, key string) (value, ok bool) {
	luaValue := table.RawGetString(key)
	if luaValue == lua.LNil {
		return false, false
	}
	boolValue, ok := luaValue.(lua.LBool)
	if !ok {
		return false, false
	}

	return bool(boolValue), true
}

func luaActionCall(value lua.LValue) ActionCall {
	if table, ok := value.(*lua.LTable); ok {
		return ActionCall{Name: luaTableString(table, "name", "")}
	}

	return ActionCall{Name: value.String()}
}
