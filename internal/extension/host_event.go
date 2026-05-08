package extension

import (
	"sort"

	lua "github.com/yuin/gopher-lua"
)

type luaHostEvent struct {
	changedWindows map[string]struct{}
	windows        map[string]WindowState
	buffers        map[string]BufferState
	uiCursor       *UICursor
	context        map[string]any
	data           map[string]any
	changedBuffers map[string]struct{}
	name           string
	key            ComposerKeyEvent
	appends        []BufferAppend
	actions        []ActionCall
	uiDrawOps      []UIDrawOp
	resetUIWindows []string
	deletedBuffers []string
	deletedWindows []string
	layout         LayoutState
	transcript     TranscriptState
	consumed       bool
	stopped        bool
	layoutChanged  bool
}

func newLuaHostEvent(event *TerminalEvent) *luaHostEvent {
	return &luaHostEvent{
		name:           event.Name,
		key:            event.Key,
		buffers:        cloneBuffers(event.Buffers),
		windows:        cloneWindows(event.Windows),
		layout:         cloneLayout(event.Layout),
		transcript:     cloneTranscript(event.Transcript),
		context:        cloneMap(event.Context),
		data:           cloneMap(event.Data),
		changedBuffers: map[string]struct{}{},
		changedWindows: map[string]struct{}{},
		appends:        []BufferAppend{},
		actions:        []ActionCall{},
		uiDrawOps:      []UIDrawOp{},
		resetUIWindows: []string{},
		deletedBuffers: []string{},
		deletedWindows: []string{},
		uiCursor:       nil,
		consumed:       false,
		stopped:        false,
		layoutChanged:  false,
	}
}

func (event *luaHostEvent) result() TerminalEventResult {
	return TerminalEventResult{
		Buffers:        cloneChangedBuffers(event.buffers, event.changedBuffers),
		Windows:        cloneChangedWindows(event.windows, event.changedWindows),
		Layout:         event.resultLayout(),
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
		Buffers:    cloneBuffers(event.buffers),
		Windows:    cloneWindows(event.windows),
		Layout:     cloneLayout(event.layout),
		Transcript: cloneTranscript(event.transcript),
		Context:    cloneMap(event.context),
		Data:       cloneMap(event.data),
		Name:       event.name,
		Key:        event.key,
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
	event.changedBuffers[name] = struct{}{}
	event.removeDeletedBuffer(name)
}

func (event *luaHostEvent) deleteBuffer(name string) {
	delete(event.buffers, name)
	delete(event.changedBuffers, name)
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
			Renderer:  "",
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
	event.layout.Windows[name] = *window
	event.changedWindows[name] = struct{}{}
	event.removeDeletedWindow(name)
}

func (event *luaHostEvent) setLayout(layout *LayoutState) {
	if layout.Windows == nil {
		layout.Windows = map[string]WindowState{}
	}
	event.layout = cloneLayout(*layout)
	event.windows = cloneWindows(event.layout.Windows)
	event.layoutChanged = true
}

func (event *luaHostEvent) deleteWindow(name string) {
	delete(event.windows, name)
	delete(event.layout.Windows, name)
	delete(event.changedWindows, name)
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
	event.applyLuaResultLayout(table.RawGetString("layout"))
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

func (event *luaHostEvent) applyLuaResultLayout(value lua.LValue) {
	layout := luaLayoutState(value)
	if layout == nil {
		return
	}
	event.setLayout(layout)
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
		cloned[name] = cloneBuffer(&buffer, name)
	}

	return cloned
}

func cloneChangedBuffers(
	buffers map[string]BufferState,
	changed map[string]struct{},
) map[string]BufferState {
	cloned := make(map[string]BufferState, len(changed))
	for name := range changed {
		if buffer, ok := buffers[name]; ok {
			cloned[name] = cloneBuffer(&buffer, name)
		}
	}

	return cloned
}

func cloneBuffer(buffer *BufferState, name string) BufferState {
	cloned := *buffer
	if cloned.Name == "" {
		cloned.Name = name
	}
	cloned.Chars = append([]string{}, cloned.Chars...)
	cloned.Metadata = cloneMap(cloned.Metadata)

	return cloned
}

func cloneWindows(windows map[string]WindowState) map[string]WindowState {
	if windows == nil {
		return map[string]WindowState{}
	}
	cloned := make(map[string]WindowState, len(windows))
	for name := range windows {
		window := windows[name]
		cloned[name] = cloneWindow(&window, name)
	}

	return cloned
}

func cloneChangedWindows(
	windows map[string]WindowState,
	changed map[string]struct{},
) map[string]WindowState {
	cloned := make(map[string]WindowState, len(changed))
	for name := range changed {
		if window, ok := windows[name]; ok {
			cloned[name] = cloneWindow(&window, name)
		}
	}

	return cloned
}

func cloneWindow(window *WindowState, name string) WindowState {
	cloned := *window
	if cloned.Name == "" {
		cloned.Name = name
	}
	cloned.Metadata = cloneMap(cloned.Metadata)

	return cloned
}

func (event *luaHostEvent) resultLayout() *LayoutState {
	if !event.layoutChanged {
		return nil
	}

	return cloneLayoutPtr(&event.layout)
}

func cloneLayout(layout LayoutState) LayoutState {
	return LayoutState{
		Windows: cloneWindows(layout.Windows),
		Width:   layout.Width,
		Height:  layout.Height,
	}
}

func cloneLayoutPtr(layout *LayoutState) *LayoutState {
	if layout == nil {
		return nil
	}
	cloned := cloneLayout(*layout)

	return &cloned
}

func cloneTranscript(transcript TranscriptState) TranscriptState {
	blocks := make([]TranscriptBlock, len(transcript.Blocks))
	for index := range transcript.Blocks {
		blocks[index] = cloneTranscriptBlock(&transcript.Blocks[index])
	}
	return TranscriptState{
		Metadata: cloneMap(transcript.Metadata),
		Blocks:   blocks,
		Count:    transcript.Count,
		Start:    transcript.Start,
		Limit:    transcript.Limit,
	}
}

func cloneTranscriptBlock(block *TranscriptBlock) TranscriptBlock {
	cloned := *block
	cloned.Metadata = cloneMap(cloned.Metadata)

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
