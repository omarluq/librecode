package extension

import (
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func (manager *Manager) luaBufferAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"append":        manager.luaBufferAppend(extensionRuntime),
		"clear":         manager.luaBufferClear(extensionRuntime),
		luaFieldCreate:  manager.luaBufferCreate(extensionRuntime),
		"delete":        manager.luaBufferDelete(extensionRuntime),
		"delete_blocks": manager.luaBufferDeleteBlocks(extensionRuntime),
		"delete_range":  manager.luaBufferDeleteRange(extensionRuntime),
		"delete_text":   manager.luaBufferDeleteRange(extensionRuntime),
		luaFieldGet:     manager.luaBufferGet(extensionRuntime),
		"get_blocks":    manager.luaBufferGetBlocks(extensionRuntime),
		"get_cursor":    manager.luaBufferGetCursor(extensionRuntime),
		"get_lines":     manager.luaBufferGetLines(extensionRuntime),
		"get_text":      manager.luaBufferGetText(extensionRuntime),
		"get_var":       manager.luaBufferGetVar(extensionRuntime),
		"insert":        manager.luaBufferInsert(extensionRuntime),
		"list":          manager.luaBufferList(extensionRuntime),
		"replace":       manager.luaBufferReplace(extensionRuntime),
		luaFieldSet:     manager.luaBufferSet(extensionRuntime),
		"set_blocks":    manager.luaBufferSetBlocks(extensionRuntime),
		"set_cursor":    manager.luaBufferSetCursor(extensionRuntime),
		"set_lines":     manager.luaBufferSetLines(extensionRuntime),
		"set_text":      manager.luaBufferSetText(extensionRuntime),
		"set_var":       manager.luaBufferSetVar(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaEventAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"consume": manager.luaEventConsume(extensionRuntime),
		"stop":    manager.luaEventStop(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaActionAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"run": manager.luaActionRun(extensionRuntime),
	})

	return apiTable
}

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

func (manager *Manager) luaUIAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"clear_region": manager.luaUIClearRegion(extensionRuntime),
		"clear_window": manager.luaUIClearWindow(extensionRuntime),
		"draw_box":     manager.luaUIDrawBox(extensionRuntime),
		"draw_lines":   manager.luaUIDrawLines(extensionRuntime),
		"draw_spans":   manager.luaUIDrawSpans(extensionRuntime),
		"draw_text":    manager.luaUIDrawText(extensionRuntime),
		"measure":      manager.luaUIMeasure(),
		"pad_right":    manager.luaUIPadRight(),
		"set_cursor":   manager.luaUISetCursor(extensionRuntime),
		"truncate":     manager.luaUITruncate(),
		"viewport":     manager.luaUIViewport(),
		"wrap":         manager.luaUIWrap(),
	})

	return apiTable
}

func (manager *Manager) luaLayoutAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		luaFieldGet: manager.luaLayoutGet(extensionRuntime),
		luaFieldSet: manager.luaLayoutSet(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaWindowAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		luaFieldCreate: manager.luaWindowCreate(extensionRuntime),
		"delete":       manager.luaWindowDelete(extensionRuntime),
		"find":         manager.luaWindowFind(extensionRuntime),
		luaFieldGet:    manager.luaWindowGet(extensionRuntime),
		"get_buf":      manager.luaWindowGetBuffer(extensionRuntime),
		"get_buffer":   manager.luaWindowGetBuffer(extensionRuntime),
		"get_var":      manager.luaWindowGetVar(extensionRuntime),
		"list":         manager.luaWindowList(extensionRuntime),
		luaFieldSet:    manager.luaWindowSet(extensionRuntime),
		"set_buf":      manager.luaWindowSetBuffer(extensionRuntime),
		"set_buffer":   manager.luaWindowSetBuffer(extensionRuntime),
		"set_renderer": manager.luaWindowSetRenderer(extensionRuntime),
		"set_var":      manager.luaWindowSetVar(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaLayoutGet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		hostEvent := checkActiveEvent(state, extensionRuntime)
		state.Push(mapToLuaTable(state, layoutForLua(&hostEvent.layout)))

		return 1
	}
}

func (manager *Manager) luaLayoutSet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		layout := luaLayoutState(state.CheckAny(1))
		if layout == nil {
			state.RaiseError("layout.set expects a layout table")
			return 0
		}
		checkActiveEvent(state, extensionRuntime).setLayout(layout)

		return 0
	}
}

func (manager *Manager) luaWindowList(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		hostEvent := checkActiveEvent(state, extensionRuntime)
		state.Push(stringSliceToLuaTable(state, hostEvent.windowNames()))

		return 1
	}
}

func (manager *Manager) luaWindowCreate(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		value := state.Get(2)
		window := WindowState{
			Metadata:  map[string]any{},
			Name:      name,
			Role:      "",
			Buffer:    "",
			Renderer:  "",
			X:         0,
			Y:         0,
			Width:     0,
			Height:    0,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		}
		if value != lua.LNil {
			window = luaWindowState(name, value)
		}
		checkActiveEvent(state, extensionRuntime).setWindow(name, &window)
		state.Push(mapToLuaTable(state, windowForLua(&window)))

		return 1
	}
}

func (manager *Manager) luaWindowDelete(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		checkActiveEvent(state, extensionRuntime).deleteWindow(name)

		return 0
	}
}

func (manager *Manager) luaWindowGet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		window, ok := checkActiveEvent(state, extensionRuntime).window(name)
		if !ok {
			state.Push(lua.LNil)
			return 1
		}
		state.Push(mapToLuaTable(state, windowForLua(&window)))

		return 1
	}
}

func (manager *Manager) luaWindowSet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		window := luaWindowState(name, state.CheckAny(2))
		checkActiveEvent(state, extensionRuntime).setWindow(name, &window)

		return 0
	}
}

func (manager *Manager) luaWindowFind(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		opts := state.CheckTable(1)
		name := luaTableString(opts, "name", "")
		role := luaTableString(opts, "role", "")
		buffer := luaTableString(opts, "buffer", "")
		windows := checkActiveEvent(state, extensionRuntime).windows
		for windowName := range windows {
			window := windows[windowName]
			if name != "" && windowName != name {
				continue
			}
			if role != "" && window.Role != role {
				continue
			}
			if buffer != "" && window.Buffer != buffer {
				continue
			}
			state.Push(lua.LString(windowName))
			return 1
		}
		state.Push(lua.LNil)

		return 1
	}
}

func (manager *Manager) luaWindowGetBuffer(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		window, ok := checkActiveEvent(state, extensionRuntime).window(name)
		if !ok {
			state.Push(lua.LNil)
			return 1
		}
		state.Push(lua.LString(window.Buffer))

		return 1
	}
}

func (manager *Manager) luaWindowSetBuffer(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		bufferName := state.CheckString(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		window := hostEventWindow(hostEvent, name)
		window.Buffer = bufferName
		hostEvent.setWindow(name, &window)

		return 0
	}
}

func (manager *Manager) luaWindowSetRenderer(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		renderer := state.CheckString(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		window := hostEventWindow(hostEvent, name)
		window.Renderer = renderer
		hostEvent.setWindow(name, &window)

		return 0
	}
}

func (manager *Manager) luaWindowGetVar(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		key := state.CheckString(2)
		window, ok := checkActiveEvent(state, extensionRuntime).window(name)
		if !ok {
			state.Push(lua.LNil)
			return 1
		}
		state.Push(goValueToLua(state, window.Metadata[key]))

		return 1
	}
}

func (manager *Manager) luaWindowSetVar(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		key := state.CheckString(2)
		value := state.CheckAny(3)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		window := hostEventWindow(hostEvent, name)
		if window.Metadata == nil {
			window.Metadata = map[string]any{}
		}
		window.Metadata[key] = luaValueToGo(value)
		hostEvent.setWindow(name, &window)

		return 0
	}
}

func hostEventWindow(hostEvent *luaHostEvent, name string) WindowState {
	window, ok := hostEvent.window(name)
	if ok {
		return window
	}

	return WindowState{
		Metadata:  map[string]any{},
		Name:      name,
		Role:      "",
		Buffer:    "",
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

func (manager *Manager) luaBufferList(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		hostEvent := checkActiveEvent(state, extensionRuntime)
		state.Push(stringSliceToLuaTable(state, hostEvent.bufferNames()))

		return 1
	}
}

func (manager *Manager) luaBufferCreate(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		value := state.Get(2)
		buffer := newBufferState(name, "")
		if value != lua.LNil {
			buffer = luaBufferState(name, value)
		}
		checkActiveEvent(state, extensionRuntime).setBuffer(name, &buffer)
		state.Push(bufferStateTable(state, &buffer))

		return 1
	}
}

func (manager *Manager) luaBufferDelete(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		checkActiveEvent(state, extensionRuntime).deleteBuffer(name)

		return 0
	}
}

func (manager *Manager) luaBufferGet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		state.Push(bufferStateTable(state, &buffer))

		return 1
	}
}

func (manager *Manager) luaBufferGetText(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		state.Push(lua.LString(checkActiveEvent(state, extensionRuntime).buffer(name).Text))

		return 1
	}
}

func (manager *Manager) luaBufferGetCursor(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		state.Push(lua.LNumber(checkActiveEvent(state, extensionRuntime).buffer(name).Cursor))

		return 1
	}
}

func (manager *Manager) luaBufferGetLines(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		start := state.OptInt(2, 0)
		end := state.OptInt(3, -1)
		buffer := checkActiveEvent(state, extensionRuntime).buffer(name)
		state.Push(stringSliceToLuaTable(state, bufferLineRange(buffer.Text, start, end)))

		return 1
	}
}

func (manager *Manager) luaBufferSet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		value := state.CheckAny(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := luaBufferState(name, value)
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferSetText(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		text := state.CheckString(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Text = text
		buffer.Chars = stringChars(text)
		buffer.Cursor = len([]rune(text))
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferClear(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Text = ""
		buffer.Chars = []string{}
		buffer.Blocks = []BufferBlock{}
		buffer.Cursor = 0
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferInsert(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		position := state.CheckInt(2)
		text := state.CheckString(3)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Text = spliceBufferText(buffer.Text, position, position, text)
		buffer.Chars = stringChars(buffer.Text)
		buffer.Cursor = clampRuneIndex(position+len([]rune(text)), len([]rune(buffer.Text)))
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferDeleteRange(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		start := state.CheckInt(2)
		end := state.CheckInt(3)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Text = spliceBufferText(buffer.Text, start, end, "")
		buffer.Chars = stringChars(buffer.Text)
		buffer.Cursor = clampRuneIndex(minInt(start, end), len([]rune(buffer.Text)))
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferReplace(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		start := state.CheckInt(2)
		end := state.CheckInt(3)
		replacement := state.CheckString(4)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Text = spliceBufferText(buffer.Text, start, end, replacement)
		buffer.Chars = stringChars(buffer.Text)
		buffer.Cursor = clampRuneIndex(minInt(start, end)+len([]rune(replacement)), len([]rune(buffer.Text)))
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferAppend(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		value := state.CheckAny(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		if table, ok := value.(*lua.LTable); ok && isBufferBlockTable(table) {
			buffer.Blocks = append(buffer.Blocks, luaBufferBlock(table, len(buffer.Blocks)))
		} else {
			buffer.Text += value.String()
			buffer.Chars = stringChars(buffer.Text)
			buffer.Cursor = len([]rune(buffer.Text))
		}
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferSetCursor(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		cursor := state.CheckInt(2)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Cursor = cursor
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferSetLines(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		start := state.CheckInt(2)
		end := state.CheckInt(3)
		replacement := luaStringSlice(state.CheckTable(4))
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Text = replaceBufferLines(buffer.Text, start, end, replacement)
		buffer.Chars = stringChars(buffer.Text)
		buffer.Cursor = minInt(buffer.Cursor, len([]rune(buffer.Text)))
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferGetBlocks(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		start := state.OptInt(2, 0)
		end := state.OptInt(3, -1)
		buffer := checkActiveEvent(state, extensionRuntime).buffer(name)
		state.Push(bufferBlocksTable(state, bufferBlockRange(buffer.Blocks, start, end)))

		return 1
	}
}

func (manager *Manager) luaBufferSetBlocks(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		start := state.CheckInt(2)
		end := state.CheckInt(3)
		replacement := luaBufferBlocks(state.CheckTable(4))
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Blocks = replaceBufferBlocks(buffer.Blocks, start, end, replacement)
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferDeleteBlocks(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		start := state.CheckInt(2)
		end := state.CheckInt(3)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		buffer.Blocks = replaceBufferBlocks(buffer.Blocks, start, end, []BufferBlock{})
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
}

func (manager *Manager) luaBufferGetVar(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		key := state.CheckString(2)
		buffer := checkActiveEvent(state, extensionRuntime).buffer(name)
		state.Push(goValueToLua(state, buffer.Metadata[key]))

		return 1
	}
}

func (manager *Manager) luaBufferSetVar(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		key := state.CheckString(2)
		value := state.CheckAny(3)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		buffer := hostEvent.buffer(name)
		if buffer.Metadata == nil {
			buffer.Metadata = map[string]any{}
		}
		buffer.Metadata[key] = luaValueToGo(value)
		hostEvent.setBuffer(name, &buffer)

		return 0
	}
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

func (manager *Manager) luaActionRun(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		checkActiveEvent(state, extensionRuntime).appendAction(ActionCall{Name: name})

		return 0
	}
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

func checkActiveEvent(state *lua.LState, extensionRuntime *luaExtension) *luaHostEvent {
	if extensionRuntime.activeEvent == nil {
		state.RaiseError("librecode runtime buffer API called outside an event")
	}

	return extensionRuntime.activeEvent
}

func bufferStateTable(state *lua.LState, buffer *BufferState) *lua.LTable {
	return mapToLuaTable(state, bufferForLua(buffer))
}

func bufferLineRange(text string, start, end int) []string {
	lines := strings.Split(text, "\n")
	start, end = normalizeLineRange(len(lines), start, end)

	return append([]string{}, lines[start:end]...)
}

func replaceBufferLines(text string, start, end int, replacement []string) string {
	lines := strings.Split(text, "\n")
	start, end = normalizeLineRange(len(lines), start, end)
	nextLines := make([]string, 0, len(lines)-end+start+len(replacement))
	nextLines = append(nextLines, lines[:start]...)
	nextLines = append(nextLines, replacement...)
	nextLines = append(nextLines, lines[end:]...)

	return strings.Join(nextLines, "\n")
}

func bufferBlockRange(blocks []BufferBlock, start, end int) []BufferBlock {
	start, end = normalizeLineRange(len(blocks), start, end)
	return cloneBufferBlocks(blocks[start:end])
}

func replaceBufferBlocks(
	blocks []BufferBlock,
	start int,
	end int,
	replacement []BufferBlock,
) []BufferBlock {
	start, end = normalizeLineRange(len(blocks), start, end)
	nextBlocks := make([]BufferBlock, 0, len(blocks)-end+start+len(replacement))
	nextBlocks = append(nextBlocks, blocks[:start]...)
	nextBlocks = append(nextBlocks, replacement...)
	nextBlocks = append(nextBlocks, blocks[end:]...)
	for index := range nextBlocks {
		nextBlocks[index].Index = index
	}

	return nextBlocks
}

func spliceBufferText(text string, start, end int, replacement string) string {
	runes := []rune(text)
	start, end = normalizeRuneRange(len(runes), start, end)
	nextRunes := make([]rune, 0, len(runes)-(end-start)+len([]rune(replacement)))
	nextRunes = append(nextRunes, runes[:start]...)
	nextRunes = append(nextRunes, []rune(replacement)...)
	nextRunes = append(nextRunes, runes[end:]...)

	return string(nextRunes)
}

func normalizeLineRange(lineCount, start, end int) (normalizedStart, normalizedEnd int) {
	normalizedStart = clampInt(start, 0, lineCount)
	normalizedEnd = end
	if normalizedEnd < 0 || normalizedEnd > lineCount {
		normalizedEnd = lineCount
	}
	normalizedEnd = clampInt(normalizedEnd, normalizedStart, lineCount)

	return normalizedStart, normalizedEnd
}

func normalizeRuneRange(runeCount, start, end int) (normalizedStart, normalizedEnd int) {
	normalizedStart = clampRuneIndex(start, runeCount)
	normalizedEnd = clampRuneIndex(end, runeCount)
	if normalizedEnd < normalizedStart {
		normalizedStart, normalizedEnd = normalizedEnd, normalizedStart
	}

	return normalizedStart, normalizedEnd
}

func clampRuneIndex(index, runeCount int) int {
	if index < 0 {
		return 0
	}
	if index > runeCount {
		return runeCount
	}

	return index
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}

	return value
}

func minInt(left, right int) int {
	if left < right {
		return left
	}

	return right
}
