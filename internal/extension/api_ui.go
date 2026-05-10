package extension

import (
	lua "github.com/yuin/gopher-lua"
)

func (manager *Manager) luaUIAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"clear_region": manager.luaUIClearRegion(extensionRuntime),
		"clear_window": manager.luaUIClearWindow(extensionRuntime),
		"draw_batch":   manager.luaUIDrawBatch(extensionRuntime),
		"draw_box":     manager.luaUIDrawBox(extensionRuntime),
		"draw_lines":   manager.luaUIDrawLines(extensionRuntime),
		"draw_spans":   manager.luaUIDrawSpans(extensionRuntime),
		"draw_text":    manager.luaUIDrawText(extensionRuntime),
		"measure":      manager.luaUIMeasure(),
		"pad_right":    manager.luaUIPadRight(),
		"set_cursor":   manager.luaUISetCursor(extensionRuntime),
		"theme_tokens": manager.luaUIThemeTokens(),
		"truncate":     manager.luaUITruncate(),
		"viewport":     manager.luaUIViewport(),
		"virtual_list": manager.luaUIVirtualList(),
		"wrap":         manager.luaUIWrap(),
	})

	return apiTable
}

func (manager *Manager) luaUIClearWindow(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		windowName := state.CheckString(1)
		checkActiveEvent(state, extensionRuntime).resetWindowUI(windowName)

		return 0
	}
}

func (manager *Manager) luaUIClearRegion(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		windowName := state.CheckString(1)
		row := state.CheckInt(2)
		col := state.CheckInt(3)
		height := state.CheckInt(4)
		width := state.CheckInt(5)
		style := luaOptionalUIStyle(state, 6)
		checkActiveEvent(state, extensionRuntime).appendUIDrawOp(&UIDrawOp{
			Style:  style,
			Spans:  []UISpan{},
			Window: windowName,
			Kind:   UIDrawKindClear,
			Text:   "",
			Row:    row,
			Col:    col,
			Width:  width,
			Height: height,
			Clear:  true,
		})

		return 0
	}
}

func (manager *Manager) luaUIMeasure() lua.LGFunction {
	return func(state *lua.LState) int {
		state.Push(lua.LNumber(uiTextWidth(state.CheckString(1))))

		return 1
	}
}

func (manager *Manager) luaUITruncate() lua.LGFunction {
	return func(state *lua.LState) int {
		state.Push(lua.LString(uiTextTruncate(state.CheckString(1), state.CheckInt(2))))

		return 1
	}
}

func (manager *Manager) luaUIPadRight() lua.LGFunction {
	return func(state *lua.LState) int {
		state.Push(lua.LString(uiTextPadRight(state.CheckString(1), state.CheckInt(2))))

		return 1
	}
}

func (manager *Manager) luaUIWrap() lua.LGFunction {
	return func(state *lua.LState) int {
		state.Push(stringSliceToLuaTable(state, uiTextWrap(state.CheckString(1), state.CheckInt(2))))

		return 1
	}
}

func (manager *Manager) luaUIViewport() lua.LGFunction {
	return func(state *lua.LState) int {
		lines := luaStringSlice(state.CheckTable(1))
		height := state.CheckInt(2)
		offset := state.OptInt(3, 0)
		visible, start, end, maxOffset := uiTextViewport(lines, height, offset)
		state.Push(mapToLuaTable(state, map[string]any{
			"lines":      visible,
			"start":      start,
			"end":        end,
			"offset":     clampInt(offset, 0, maxOffset),
			"max_offset": maxOffset,
			"total":      len(lines),
		}))

		return 1
	}
}

func (manager *Manager) luaUIVirtualList() lua.LGFunction {
	return func(state *lua.LState) int {
		items := state.CheckTable(1)
		height := state.CheckInt(2)
		offset := state.OptInt(3, 0)
		result := uiVirtualList(luaVirtualListHeights(items), height, offset)
		state.Push(mapToLuaTable(state, map[string]any{
			"items":      uiVirtualListItemsForLua(result.Items),
			"start":      result.Start,
			"end":        result.End,
			"offset":     result.Offset,
			"max_offset": result.MaxOffset,
			"total":      result.Total,
		}))

		return 1
	}
}

func (manager *Manager) luaUIThemeTokens() lua.LGFunction {
	return func(state *lua.LState) int {
		state.Push(stringSliceToLuaTable(state, uiThemeTokens()))

		return 1
	}
}

func (manager *Manager) luaUIDrawText(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		windowName := state.CheckString(1)
		row := state.CheckInt(2)
		col := state.CheckInt(3)
		text := state.CheckString(4)
		style := luaOptionalUIStyle(state, 5)
		checkActiveEvent(state, extensionRuntime).appendUIDrawOp(&UIDrawOp{
			Style:  style,
			Spans:  []UISpan{},
			Window: windowName,
			Kind:   UIDrawKindText,
			Text:   text,
			Row:    row,
			Col:    col,
			Width:  0,
			Height: 0,
			Clear:  false,
		})

		return 0
	}
}

func (manager *Manager) luaUIDrawBatch(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		operations := state.CheckTable(1)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		for operationIndex := 1; operationIndex <= operations.Len(); operationIndex++ {
			hostEvent.appendUIDrawOp(luaUIDrawOp(operations.RawGetInt(operationIndex)))
		}

		return 0
	}
}

func (manager *Manager) luaUIDrawLines(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		windowName := state.CheckString(1)
		row := state.CheckInt(2)
		col := state.CheckInt(3)
		lines := luaStringSlice(state.CheckTable(4))
		style := luaOptionalUIStyle(state, 5)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		for index, line := range lines {
			hostEvent.appendUIDrawOp(&UIDrawOp{
				Style:  style,
				Spans:  []UISpan{},
				Window: windowName,
				Kind:   UIDrawKindText,
				Text:   line,
				Row:    row + index,
				Col:    col,
				Width:  0,
				Height: 0,
				Clear:  false,
			})
		}

		return 0
	}
}

func (manager *Manager) luaUIDrawSpans(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		windowName := state.CheckString(1)
		row := state.CheckInt(2)
		col := state.CheckInt(3)
		spans := luaUISpans(state.CheckTable(4))
		checkActiveEvent(state, extensionRuntime).appendUIDrawOp(&UIDrawOp{
			Style:  UIStyle{FG: "", BG: "", Bold: false, Italic: false},
			Spans:  spans,
			Window: windowName,
			Kind:   UIDrawKindSpans,
			Text:   "",
			Row:    row,
			Col:    col,
			Width:  0,
			Height: 0,
			Clear:  false,
		})

		return 0
	}
}

func (manager *Manager) luaUIDrawBox(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		windowName := state.CheckString(1)
		style := luaOptionalUIStyle(state, 2)
		checkActiveEvent(state, extensionRuntime).appendUIDrawOp(&UIDrawOp{
			Style:  style,
			Spans:  []UISpan{},
			Window: windowName,
			Kind:   UIDrawKindBox,
			Text:   "",
			Row:    0,
			Col:    0,
			Width:  0,
			Height: 0,
			Clear:  false,
		})

		return 0
	}
}

func (manager *Manager) luaUISetCursor(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		windowName := state.CheckString(1)
		row := state.CheckInt(2)
		col := state.CheckInt(3)
		checkActiveEvent(state, extensionRuntime).setUICursor(&UICursor{Window: windowName, Row: row, Col: col})

		return 0
	}
}

func luaVirtualListHeights(items *lua.LTable) []int {
	heights := make([]int, 0, items.Len())
	for index := 1; index <= items.Len(); index++ {
		heights = append(heights, luaVirtualListItemHeight(items.RawGetInt(index)))
	}

	return heights
}

func luaVirtualListItemHeight(value lua.LValue) int {
	if number, ok := value.(lua.LNumber); ok {
		return positiveInt(int(number))
	}
	if table, ok := value.(*lua.LTable); ok {
		return positiveInt(luaTableIntValueWithDefault(table, luaFieldHeight, 1))
	}

	return 1
}

func luaOptionalUIStyle(state *lua.LState, index int) UIStyle {
	value := state.Get(index)
	table, ok := value.(*lua.LTable)
	if !ok {
		return UIStyle{FG: "", BG: "", Bold: false, Italic: false}
	}

	return luaUIStyle(table)
}

func luaUIStyle(table *lua.LTable) UIStyle {
	return UIStyle{
		FG:     luaTableString(table, "fg", ""),
		BG:     luaTableString(table, "bg", ""),
		Bold:   luaTableBool(table, "bold"),
		Italic: luaTableBool(table, "italic"),
	}
}

func luaUISpans(table *lua.LTable) []UISpan {
	spans := make([]UISpan, 0, table.Len())
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		if spanTable, ok := table.RawGetInt(valueIndex).(*lua.LTable); ok {
			spans = append(spans, UISpan{
				Text:  luaTableString(spanTable, luaFieldText, ""),
				Style: luaUIStyle(spanTable),
			})
		}
	}

	return spans
}
