package extension

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/omarluq/librecode/internal/tool"

	lua "github.com/yuin/gopher-lua"
)

const (
	luaBufferComposer = "composer"

	luaFieldActions        = "actions"
	luaFieldBuffers        = "buffers"
	luaFieldDeletedBuffers = "deleted_buffers"
	luaFieldDeletedWindows = "deleted_windows"
	luaFieldLayout         = "layout"
	luaFieldResetUIWindows = "reset_ui_windows"
	luaFieldUICursor       = "ui_cursor"
	luaFieldUIDrawOps      = "ui_draw_ops"

	luaFieldCreate   = "create"
	luaFieldGet      = "get"
	luaFieldHeight   = "height"
	luaFieldKey      = "key"
	luaFieldKind     = "kind"
	luaFieldMetadata = "metadata"
	luaFieldName     = "name"
	luaFieldSet      = "set"
	luaFieldText     = "text"
	luaFieldWidth    = "width"
	luaFieldWindows  = "windows"
)

func toolArgumentsTable(state *lua.LState, arguments tool.Arguments) *lua.LTable {
	fields, err := arguments.Fields()
	if err != nil {
		return state.NewTable()
	}

	table := state.NewTable()
	for key, raw := range fields {
		jsonRawToLuaValue(state, raw).SetField(table, key)
	}

	return table
}

func jsonRawToLuaValue(state *lua.LState, raw json.RawMessage) luaValue {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return luaValue{value: lua.LNil}
	}

	return newLuaValue(state, value)
}

func mapToLuaTable(state *lua.LState, values map[string]any) *lua.LTable {
	table := state.NewTable()
	for key, value := range values {
		newLuaValue(state, value).SetField(table, key)
	}

	return table
}

type luaValue struct {
	value lua.LValue
}

func newLuaValue(state *lua.LState, value any) luaValue {
	if scalar, ok := scalarLuaValue(value); ok {
		return scalar
	}

	switch typedValue := value.(type) {
	case map[string]any:
		return luaValue{value: mapToLuaTable(state, typedValue)}
	case map[string]string:
		return luaValue{value: stringMapToLuaTable(state, typedValue)}
	case []any:
		return luaValue{value: sliceToLuaTable(state, typedValue)}
	case []string:
		return luaValue{value: stringSliceToLuaTable(state, typedValue)}
	default:
		return luaValue{value: lua.LString(fmt.Sprint(typedValue))}
	}
}

func scalarLuaValue(value any) (luaValue, bool) {
	switch typedValue := value.(type) {
	case nil:
		return luaValue{value: lua.LNil}, true
	case string:
		return luaValue{value: lua.LString(typedValue)}, true
	case bool:
		return luaValue{value: lua.LBool(typedValue)}, true
	case int:
		return luaValue{value: lua.LNumber(typedValue)}, true
	case int64:
		return luaValue{value: lua.LNumber(typedValue)}, true
	case float64:
		return luaValue{value: lua.LNumber(typedValue)}, true
	default:
		return luaValue{value: nil}, false
	}
}

func (value luaValue) SetField(table *lua.LTable, key string) {
	table.RawSetString(key, value.value)
}

func (value luaValue) RawSetInt(table *lua.LTable, index int) {
	table.RawSetInt(index, value.value)
}

func (value luaValue) Push(state *lua.LState) {
	state.Push(value.value)
}

func sliceToLuaTable(state *lua.LState, values []any) *lua.LTable {
	table := state.NewTable()
	for valueIndex, value := range values {
		newLuaValue(state, value).RawSetInt(table, valueIndex+1)
	}

	return table
}

func stringMapToLuaTable(state *lua.LState, values map[string]string) *lua.LTable {
	table := state.NewTable()
	for key, value := range values {
		state.SetField(table, key, lua.LString(value))
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

	table.ForEach(func(key, value lua.LValue) {
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
		luaFieldName:    event.Name,
		luaFieldKey:     event.Key.Key,
		luaFieldText:    event.Key.Text,
		"ctrl":          event.Key.Ctrl,
		"alt":           event.Key.Alt,
		"shift":         event.Key.Shift,
		"context":       event.Context,
		"data":          event.Data,
		"buffers":       bufferMapForLua(event.Buffers),
		luaFieldWindows: windowMapForLua(event.Windows),
		"layout":        layoutForLua(&event.Layout),
		"focus":         focusForLua(&event.Focus),
		"composer":      bufferForLua(&composer),
		"working":       luaContextBool(event.Context, "working"),
		"auth_working":  luaContextBool(event.Context, "auth_working"),
	})
}

func focusForLua(focus *FocusState) map[string]any {
	return map[string]any{
		luaFieldKind:      focus.Kind,
		keymapScopeWindow: focus.Window,
		keymapScopeBuffer: focus.Buffer,
		keymapScopeRole:   focus.Role,
		"panel_kind":      focus.PanelKind,
		"exclusive":       focus.Exclusive,
	}
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
		luaFieldWindows: windowMapForLua(layout.Windows),
		luaFieldWidth:   layout.Width,
		luaFieldHeight:  layout.Height,
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
		luaFieldName:      window.Name,
		"role":            window.Role,
		keymapScopeBuffer: window.Buffer,
		"renderer":        window.Renderer,
		"x":               window.X,
		"y":               window.Y,
		luaFieldWidth:     window.Width,
		luaFieldHeight:    window.Height,
		"cursor_row":      window.CursorRow,
		"cursor_col":      window.CursorCol,
		"visible":         window.Visible,
		luaFieldMetadata:  window.Metadata,
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
		luaFieldKind:     block.Kind,
		"role":           block.Role,
		luaFieldText:     block.Text,
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
			Name:     luaTableString(table, luaFieldName, name),
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
		Kind:      luaTableString(table, luaFieldKind, ""),
		Role:      luaTableString(table, "role", ""),
		Text:      luaTableString(table, luaFieldText, ""),
		Index:     luaTableIntValueWithDefault(table, "index", index),
		Streaming: luaTableBool(table, "streaming"),
	}
}

func isBufferBlockTable(table *lua.LTable) bool {
	return table.RawGetString(luaFieldKind) != lua.LNil ||
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
			Name:      luaTableString(table, luaFieldName, name),
			Role:      luaTableString(table, "role", ""),
			Buffer:    luaTableString(table, keymapScopeBuffer, ""),
			Renderer:  luaTableString(table, "renderer", ""),
			X:         luaTableIntValue(table, "x"),
			Y:         luaTableIntValue(table, "y"),
			Width:     luaTableIntValue(table, luaFieldWidth),
			Height:    luaTableIntValue(table, luaFieldHeight),
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
		spans := []UISpan{}
		if spansTable, ok := table.RawGetString("spans").(*lua.LTable); ok {
			spans = luaUISpans(spansTable)
		}

		return &UIDrawOp{
			Style:  luaUIStyle(table),
			Spans:  spans,
			Window: luaTableString(table, keymapScopeWindow, ""),
			Kind:   luaTableString(table, luaFieldKind, ""),
			Text:   luaTableString(table, luaFieldText, ""),
			Row:    luaTableIntValue(table, "row"),
			Col:    luaTableIntValue(table, "col"),
			Width:  luaTableIntValue(table, luaFieldWidth),
			Height: luaTableIntValue(table, luaFieldHeight),
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
		Window: luaTableString(table, keymapScopeWindow, ""),
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

	windowsValue := table.RawGetString(luaFieldWindows)
	if windowsTable, ok := windowsValue.(*lua.LTable); ok {
		windowsTable.ForEach(func(key, windowValue lua.LValue) {
			name := key.String()
			windows[name] = luaWindowState(name, windowValue)
		})
	}

	return &LayoutState{
		Windows: windows,
		Width:   luaTableIntValue(table, luaFieldWidth),
		Height:  luaTableIntValue(table, luaFieldHeight),
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
		return ActionCall{Name: luaTableString(table, luaFieldName, "")}
	}

	return ActionCall{Name: value.String()}
}
