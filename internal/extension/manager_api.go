package extension

import (
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

func (manager *Manager) luaBufferAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"append":       manager.luaBufferAppend(extensionRuntime),
		luaFieldCreate: manager.luaBufferCreate(extensionRuntime),
		"delete":       manager.luaBufferDelete(extensionRuntime),
		"delete_range": manager.luaBufferDeleteRange(extensionRuntime),
		"delete_text":  manager.luaBufferDeleteRange(extensionRuntime),
		"get":          manager.luaBufferGet(extensionRuntime),
		"get_cursor":   manager.luaBufferGetCursor(extensionRuntime),
		"get_lines":    manager.luaBufferGetLines(extensionRuntime),
		"get_text":     manager.luaBufferGetText(extensionRuntime),
		"insert":       manager.luaBufferInsert(extensionRuntime),
		"list":         manager.luaBufferList(extensionRuntime),
		"replace":      manager.luaBufferReplace(extensionRuntime),
		"set":          manager.luaBufferSet(extensionRuntime),
		"set_cursor":   manager.luaBufferSetCursor(extensionRuntime),
		"set_lines":    manager.luaBufferSetLines(extensionRuntime),
		"set_text":     manager.luaBufferSetText(extensionRuntime),
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

func (manager *Manager) luaWindowAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"list":     manager.luaWindowList(extensionRuntime),
		"get":      manager.luaWindowGet(extensionRuntime),
		"find":     manager.luaWindowFind(extensionRuntime),
		"get_buf":  manager.luaWindowGetBuffer(extensionRuntime),
		"get_buffer": manager.luaWindowGetBuffer(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaWindowList(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		hostEvent := checkActiveEvent(state, extensionRuntime)
		names := make([]string, 0, len(hostEvent.windows))
		for name := range hostEvent.windows {
			names = append(names, name)
		}
		sort.Strings(names)
		state.Push(stringSliceToLuaTable(state, names))

		return 1
	}
}

func (manager *Manager) luaWindowGet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		hostEvent := checkActiveEvent(state, extensionRuntime)
		window, ok := hostEvent.windows[name]
		if !ok {
			state.Push(lua.LNil)
			return 1
		}
		state.Push(mapToLuaTable(state, windowForLua(&window)))

		return 1
	}
}

func (manager *Manager) luaWindowFind(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		opts := state.CheckTable(1)
		name := luaTableString(opts, "name", "")
		role := luaTableString(opts, "role", "")
		buffer := luaTableString(opts, "buffer", "")
		for windowName, window := range checkActiveEvent(state, extensionRuntime).windows {
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
		window, ok := checkActiveEvent(state, extensionRuntime).windows[name]
		if !ok {
			state.Push(lua.LNil)
			return 1
		}
		state.Push(lua.LString(window.Buffer))

		return 1
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
		hostEvent.appendBuffer(bufferAppendFromLua(name, value))

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

func checkActiveEvent(state *lua.LState, extensionRuntime *luaExtension) *luaHostEvent {
	if extensionRuntime.activeEvent == nil {
		state.RaiseError("librecode runtime buffer API called outside an event")
	}

	return extensionRuntime.activeEvent
}

func bufferStateTable(state *lua.LState, buffer *BufferState) *lua.LTable {
	return mapToLuaTable(state, bufferForLua(buffer))
}

func bufferAppendFromLua(name string, value lua.LValue) BufferAppend {
	if table, ok := value.(*lua.LTable); ok {
		bufferAppend := luaBufferAppend(table)
		if bufferAppend.Name == "" {
			bufferAppend.Name = name
		}

		return bufferAppend
	}

	return BufferAppend{
		Name: name,
		Text: value.String(),
		Role: "custom",
	}
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
