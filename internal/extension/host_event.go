package extension

import (
	"sort"

	lua "github.com/yuin/gopher-lua"
)

type luaHostEvent struct {
	buffers        map[string]BufferState
	windows        map[string]WindowState
	context        map[string]any
	appends        []BufferAppend
	actions        []ActionCall
	uiDrawOps      []UIDrawOp
	resetUIWindows []string
	deletedBuffers []string
	deletedWindows []string
	uiCursor       *UICursor
	name           string
	key            ComposerKeyEvent
	consumed       bool
	stopped        bool
}

func newLuaHostEvent(event *TerminalEvent) *luaHostEvent {
	return &luaHostEvent{
		name:           event.Name,
		key:            event.Key,
		buffers:        cloneBuffers(event.Buffers),
		windows:        cloneWindows(event.Windows),
		context:        cloneMap(event.Context),
		appends:        []BufferAppend{},
		actions:        []ActionCall{},
		uiDrawOps:      []UIDrawOp{},
		resetUIWindows: []string{},
		deletedBuffers: []string{},
		deletedWindows: []string{},
		uiCursor:       nil,
		consumed:       false,
		stopped:        false,
	}
}

func (event *luaHostEvent) result() TerminalEventResult {
	return TerminalEventResult{
		Buffers:        cloneBuffers(event.buffers),
		Windows:        cloneWindows(event.windows),
		Appends:        append([]BufferAppend{}, event.appends...),
		Actions:        append([]ActionCall{}, event.actions...),
		UIDrawOps:      append([]UIDrawOp{}, event.uiDrawOps...),
		ResetUIWindows: append([]string{}, event.resetUIWindows...),
		DeletedBuffers: append([]string{}, event.deletedBuffers...),
		DeletedWindows: append([]string{}, event.deletedWindows...),
		UICursor:       cloneUICursor(event.uiCursor),
		Consumed:       event.consumed,
	}
}

func (event *luaHostEvent) eventSnapshot() *TerminalEvent {
	return &TerminalEvent{
		Buffers: cloneBuffers(event.buffers),
		Windows: cloneWindows(event.windows),
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

func (event *luaHostEvent) windowNames() []string {
	names := make([]string, 0, len(event.windows))
	for name := range event.windows {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

func (event *luaHostEvent) window(name string) (WindowState, bool) {
	window, ok := event.windows[name]
	if !ok {
		return WindowState{
			Metadata:  map[string]any{},
			Name:      "",
			Role:      "",
			Buffer:    "",
			X:         0,
			Y:         0,
			Width:     0,
			Height:    0,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   false,
		}, false
	}
	if window.Name == "" {
		window.Name = name
	}
	if window.Metadata == nil {
		window.Metadata = map[string]any{}
	}

	return window, true
}

func (event *luaHostEvent) setWindow(name string, window *WindowState) {
	if window.Name == "" {
		window.Name = name
	}
	window.Metadata = cloneMap(window.Metadata)
	event.windows[name] = *window
	event.removeDeletedWindow(name)
}

func (event *luaHostEvent) deleteWindow(name string) {
	delete(event.windows, name)
	for _, deletedWindow := range event.deletedWindows {
		if deletedWindow == name {
			return
		}
	}
	event.deletedWindows = append(event.deletedWindows, name)
}

func (event *luaHostEvent) removeDeletedWindow(name string) {
	for index, deletedWindow := range event.deletedWindows {
		if deletedWindow == name {
			event.deletedWindows = append(event.deletedWindows[:index], event.deletedWindows[index+1:]...)
			return
		}
	}
}

func (event *luaHostEvent) appendUIDrawOp(drawOp *UIDrawOp) {
	if drawOp == nil || drawOp.Window == "" {
		return
	}
	event.uiDrawOps = append(event.uiDrawOps, *drawOp)
}

func (event *luaHostEvent) resetWindowUI(name string) {
	if name == "" {
		return
	}
	for _, windowName := range event.resetUIWindows {
		if windowName == name {
			return
		}
	}
	event.resetUIWindows = append(event.resetUIWindows, name)
}

func (event *luaHostEvent) setUICursor(cursor *UICursor) {
	event.uiCursor = cloneUICursor(cursor)
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

func (event *luaHostEvent) appendAction(action ActionCall) {
	if action.Name == "" {
		return
	}
	event.actions = append(event.actions, action)
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
	event.applyLuaResultWindows(table.RawGetString("windows"))
	event.applyLuaResultAppends(table.RawGetString("appends"))
	event.applyLuaResultActions(table.RawGetString("actions"))
	event.applyLuaResultDrawOps(table.RawGetString("ui_draw_ops"))
	event.applyLuaResultResetUI(table.RawGetString("reset_ui_windows"))
	event.applyLuaResultCursor(table.RawGetString("ui_cursor"))
	event.applyLuaResultDeletes(table.RawGetString("deleted_buffers"))
	event.applyLuaResultDeletedWindows(table.RawGetString("deleted_windows"))
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

func (event *luaHostEvent) applyLuaResultWindows(value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	table.ForEach(func(key lua.LValue, windowValue lua.LValue) {
		name := key.String()
		window := luaWindowState(name, windowValue)
		event.setWindow(name, &window)
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

func (event *luaHostEvent) applyLuaResultActions(value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		event.appendAction(luaActionCall(table.RawGetInt(valueIndex)))
	}
}

func (event *luaHostEvent) applyLuaResultDrawOps(value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		event.appendUIDrawOp(luaUIDrawOp(table.RawGetInt(valueIndex)))
	}
}

func (event *luaHostEvent) applyLuaResultResetUI(value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		event.resetWindowUI(table.RawGetInt(valueIndex).String())
	}
}

func (event *luaHostEvent) applyLuaResultCursor(value lua.LValue) {
	cursor := luaUICursor(value)
	if cursor == nil {
		return
	}
	event.setUICursor(cursor)
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

func (event *luaHostEvent) applyLuaResultDeletedWindows(value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	for valueIndex := 1; valueIndex <= table.Len(); valueIndex++ {
		event.deleteWindow(table.RawGetInt(valueIndex).String())
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

func cloneWindows(windows map[string]WindowState) map[string]WindowState {
	if windows == nil {
		return map[string]WindowState{}
	}
	cloned := make(map[string]WindowState, len(windows))
	for name, window := range windows {
		if window.Name == "" {
			window.Name = name
		}
		window.Metadata = cloneMap(window.Metadata)
		cloned[name] = window
	}

	return cloned
}

func cloneUICursor(cursor *UICursor) *UICursor {
	if cursor == nil {
		return nil
	}
	cloned := *cursor

	return &cloned
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
