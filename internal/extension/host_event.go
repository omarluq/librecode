package extension

import (
	"sort"

	lua "github.com/yuin/gopher-lua"
)

type luaHostEvent struct {
	buffers        map[string]BufferState
	context        map[string]any
	appends        []BufferAppend
	deletedBuffers []string
	name           string
	key            ComposerKeyEvent
	consumed       bool
	stopped        bool
}

func newLuaHostEvent(event TerminalEvent) *luaHostEvent {
	return &luaHostEvent{
		name:           event.Name,
		key:            event.Key,
		buffers:        cloneBuffers(event.Buffers),
		context:        cloneMap(event.Context),
		appends:        []BufferAppend{},
		deletedBuffers: []string{},
		consumed:       false,
		stopped:        false,
	}
}

func (event *luaHostEvent) result() TerminalEventResult {
	return TerminalEventResult{
		Buffers:        cloneBuffers(event.buffers),
		Appends:        append([]BufferAppend{}, event.appends...),
		DeletedBuffers: append([]string{}, event.deletedBuffers...),
		Consumed:       event.consumed,
	}
}

func (event *luaHostEvent) eventSnapshot() TerminalEvent {
	return TerminalEvent{
		Buffers: cloneBuffers(event.buffers),
		Context: cloneMap(event.context),
		Name:    event.name,
		Key:     event.key,
	}
}

func (event *luaHostEvent) buffer(name string) BufferState {
	buffer, ok := event.buffers[name]
	if !ok {
		return newBufferState(name, "")
	}
	if buffer.Name == "" {
		buffer.Name = name
	}
	if buffer.Chars == nil {
		buffer.Chars = stringChars(buffer.Text)
	}
	if buffer.Metadata == nil {
		buffer.Metadata = map[string]any{}
	}

	return buffer
}

func (event *luaHostEvent) setBuffer(name string, buffer *BufferState) {
	if buffer.Name == "" {
		buffer.Name = name
	}
	if buffer.Chars == nil {
		buffer.Chars = stringChars(buffer.Text)
	}
	buffer.Metadata = cloneMap(buffer.Metadata)
	event.buffers[name] = *buffer
	event.removeDeletedBuffer(name)
}

func (event *luaHostEvent) deleteBuffer(name string) {
	delete(event.buffers, name)
	for _, deletedBuffer := range event.deletedBuffers {
		if deletedBuffer == name {
			return
		}
	}
	event.deletedBuffers = append(event.deletedBuffers, name)
}

func (event *luaHostEvent) removeDeletedBuffer(name string) {
	for index, deletedBuffer := range event.deletedBuffers {
		if deletedBuffer == name {
			event.deletedBuffers = append(event.deletedBuffers[:index], event.deletedBuffers[index+1:]...)
			return
		}
	}
}

func (event *luaHostEvent) bufferNames() []string {
	names := make([]string, 0, len(event.buffers))
	for name := range event.buffers {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

func (event *luaHostEvent) appendBuffer(bufferAppend BufferAppend) {
	if bufferAppend.Name == "" {
		return
	}
	event.appends = append(event.appends, bufferAppend)
	buffer := event.buffer(bufferAppend.Name)
	buffer.Text += bufferAppend.Text
	buffer.Chars = stringChars(buffer.Text)
	buffer.Cursor = len([]rune(buffer.Text))
	event.setBuffer(bufferAppend.Name, &buffer)
}

func (event *luaHostEvent) applyLuaResult(value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	if luaTableBool(table, "handled") || luaTableBool(table, "consumed") {
		event.consumed = true
	}
	if luaTableBool(table, "stop") || luaTableBool(table, "stopped") {
		event.consumed = true
		event.stopped = true
	}
	event.applyLuaResultBuffers(table.RawGetString("buffers"))
	event.applyLuaResultAppends(table.RawGetString("appends"))
	event.applyLuaResultDeletes(table.RawGetString("deleted_buffers"))
}

func (event *luaHostEvent) applyLuaResultBuffers(value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	table.ForEach(func(key lua.LValue, bufferValue lua.LValue) {
		name := key.String()
		buffer := luaBufferState(name, bufferValue)
		event.setBuffer(name, &buffer)
	})
}

func (event *luaHostEvent) applyLuaResultAppends(value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		event.appendBuffer(luaBufferAppend(table.RawGetInt(valueIndex)))
	}
}

func (event *luaHostEvent) applyLuaResultDeletes(value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		event.deleteBuffer(table.RawGetInt(valueIndex).String())
	}
}

func cloneBuffers(buffers map[string]BufferState) map[string]BufferState {
	cloned := make(map[string]BufferState, len(buffers))
	for name, buffer := range buffers {
		if buffer.Name == "" {
			buffer.Name = name
		}
		buffer.Chars = append([]string{}, buffer.Chars...)
		buffer.Metadata = cloneMap(buffer.Metadata)
		cloned[name] = buffer
	}

	return cloned
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}

func newBufferState(name, text string) BufferState {
	return BufferState{
		Metadata: map[string]any{},
		Name:     name,
		Text:     text,
		Chars:    stringChars(text),
		Label:    "",
		Cursor:   len([]rune(text)),
	}
}

func stringChars(text string) []string {
	chars := make([]string, 0, len([]rune(text)))
	for _, char := range text {
		chars = append(chars, string(char))
	}

	return chars
}
