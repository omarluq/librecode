package extension

import (
	lua "github.com/yuin/gopher-lua"
	"strings"
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
