package terminal

import (
	"context"
	"maps"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/extension"
)

const (
	extensionDataAssistantEntryID = "assistant_entry_id"
	extensionDataCached           = "cached"
	extensionDataDetailsJSON      = "details_json"
	extensionDataEntryID          = "entry_id"
	extensionDataError            = "error"
	extensionDataHeight           = "height"
	extensionDataName             = "name"
	extensionDataPromptID         = "prompt_id"
	extensionDataResult           = "result"
	extensionDataSessionID        = "session_id"
	extensionDataText             = "text"
	extensionDataToolArgsJSON     = "arguments_json"
	extensionDataUserEntryID      = "user_entry_id"
	extensionDataWidth            = "width"
	extensionEventKey             = "key"
	extensionEventModelDelta      = "model_delta"
	extensionEventPromptDone      = "prompt_done"
	extensionEventPromptSubmit    = "prompt_submit"
	extensionEventPromptUser      = "prompt_user_entry"
	extensionEventRender          = "render"
	extensionEventRetryEnd        = "retry_end"
	extensionEventRetryStart      = "retry_start"
	extensionEventResize          = "resize"
	extensionEventStartup         = "startup"
	extensionEventTick            = "tick"
	extensionEventThinkingDelta   = "thinking_delta"
	extensionEventToolEnd         = "tool_end"
	extensionEventToolStart       = "tool_start"
	extensionBufferComposer       = "composer"
	extensionBufferStatus         = "status"
	extensionBufferTranscript     = "transcript"
)

func (app *App) runStartupExtensions(ctx context.Context) error {
	if !app.hasExtensionHandlers(extensionEventStartup) {
		return nil
	}

	event := app.newExtensionEvent(extensionEventStartup, extension.ComposerKeyEvent{
		Key:   "",
		Text:  "",
		Ctrl:  false,
		Alt:   false,
		Shift: false,
	})
	result, err := app.extensions.HandleTerminalEvent(
		ctx,
		&event,
	)
	if err != nil {
		return err
	}
	app.applyExtensionEventResult(ctx, &result)

	return nil
}

func (app *App) handleExtensionKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if !app.hasExtensionHandlers(extensionEventKey) {
		return false, nil
	}

	extEvent := app.newExtensionEvent(extensionEventKey, terminalKeyEvent(event))
	result, err := app.extensions.HandleTerminalEvent(
		ctx,
		&extEvent,
	)
	if err != nil {
		return false, err
	}
	app.applyExtensionEventResult(ctx, &result)

	return result.Consumed, nil
}

func (app *App) handleResizeExtensions(ctx context.Context) error {
	if !app.hasExtensionHandlers(extensionEventResize) {
		return nil
	}
	layout := app.currentRuntimeLayout()

	return app.emitExtensionRuntimeEvent(
		ctx,
		extensionEventResize,
		app.extensionRuntimeData(layout.Width, layout.Height),
	)
}

func (app *App) runRenderExtensions(ctx context.Context, layout *runtimeLayout) {
	if layout == nil {
		return
	}
	app.resetFrameUIOverrides()
	if !app.hasExtensionHandlers(extensionEventRender) {
		return
	}
	data := app.extensionRuntimeData(layout.Width, layout.Height)
	event := app.newExtensionEventWithLayoutAndData(extensionEventRender, emptyExtensionKeyEvent(), layout, data)
	result, err := app.extensions.HandleTerminalEvent(ctx, &event)
	if err != nil {
		app.addSystemMessage(err.Error())
		return
	}
	app.applyExtensionEventResult(ctx, &result)
}

func emptyExtensionKeyEvent() extension.ComposerKeyEvent {
	return extension.ComposerKeyEvent{Key: "", Text: "", Ctrl: false, Alt: false, Shift: false}
}

func (app *App) hasExtensionHandlers(name string) bool {
	if app.extensions == nil {
		return false
	}
	inspector, ok := app.extensions.(extension.TerminalEventInspector)
	if !ok {
		return true
	}

	return inspector.HasTerminalEventHandlers(name)
}

func (app *App) hasExtensionTimers() bool {
	if app.extensions == nil {
		return false
	}
	scheduler, ok := app.extensions.(extension.TimerScheduler)
	if !ok {
		return false
	}
	_, hasTimer := scheduler.NextTimerDelay(time.Now())

	return hasTimer
}

func (app *App) extensionRuntimeData(width, height int) map[string]any {
	modelText := modelLabel(app.currentProvider(), app.currentModel())

	return map[string]any{
		extensionDataWidth:      width,
		extensionDataHeight:     height,
		"model_label":           modelText,
		"provider":              app.currentProvider(),
		string(panelModel):      app.currentModel(),
		"thinking_level":        app.currentThinkingLevel(),
		"tools_expanded":        app.toolsExpanded,
		"thinking_hidden":       app.hideThinking,
		"queued_count":          len(app.queuedMessages),
		"message_count":         len(app.messages),
		"streaming_block_count": len(app.streamingBlocks),
	}
}

func (app *App) emitExtensionRuntimeEvent(ctx context.Context, name string, data map[string]any) error {
	if !app.hasExtensionHandlers(name) && !app.hasExtensionTimers() {
		return nil
	}
	event := app.newExtensionEventWithData(name, emptyExtensionKeyEvent(), data)
	result, err := app.extensions.HandleTerminalEvent(ctx, &event)
	if err != nil {
		return err
	}
	app.applyExtensionEventResult(ctx, &result)

	return nil
}

func (app *App) emitExtensionRuntimeEventOrMessage(ctx context.Context, name string, data map[string]any) {
	if err := app.emitExtensionRuntimeEvent(ctx, name, data); err != nil {
		app.addSystemMessage(err.Error())
	}
}

func (app *App) resetFrameUIOverrides() {
	app.uiWindowOverrides = map[string]uiWindowOverride{}
	app.uiCursor = nil
}

func (app *App) applyPromptSubmitExtensions(ctx context.Context) (bool, error) {
	if !app.hasExtensionHandlers(extensionEventPromptSubmit) {
		return false, nil
	}

	event := app.newExtensionEvent(extensionEventPromptSubmit, extension.ComposerKeyEvent{
		Key:   "enter",
		Text:  "",
		Ctrl:  false,
		Alt:   false,
		Shift: false,
	})
	result, err := app.extensions.HandleTerminalEvent(
		ctx,
		&event,
	)
	if err != nil {
		return false, err
	}
	app.applyExtensionEventResult(ctx, &result)

	return result.Consumed, nil
}

func (app *App) newExtensionEvent(name string, key extension.ComposerKeyEvent) extension.TerminalEvent {
	return app.newExtensionEventWithData(name, key, nil)
}

func (app *App) newExtensionEventWithData(
	name string,
	key extension.ComposerKeyEvent,
	data map[string]any,
) extension.TerminalEvent {
	layout := app.currentRuntimeLayout()
	return app.newExtensionEventWithLayoutAndData(name, key, &layout, data)
}

func (app *App) newExtensionEventWithLayoutAndData(
	name string,
	key extension.ComposerKeyEvent,
	layout *runtimeLayout,
	data map[string]any,
) extension.TerminalEvent {
	windows := app.cloneRuntimeWindows(layout)
	return extension.TerminalEvent{
		Buffers: app.extensionBuffers(),
		Windows: windows,
		Layout:  extension.LayoutState{Windows: windows, Width: layout.Width, Height: layout.Height},
		Context: app.extensionContext(),
		Data:    cloneExtensionMetadata(data),
		Name:    name,
		Key:     key,
	}
}

func (app *App) extensionBuffers() map[string]extension.BufferState {
	reservedBuffers := app.reservedRuntimeBuffers()
	buffers := make(map[string]extension.BufferState, len(app.extensionRuntimeBuffers)+len(reservedBuffers))
	for name, buffer := range app.extensionRuntimeBuffers {
		buffers[name] = cloneRuntimeBufferState(name, &buffer)
	}
	maps.Copy(buffers, reservedBuffers)

	return buffers
}

func (app *App) composerBufferState() extension.BufferState {
	return cloneBufferState(&app.composerBuffer)
}

func cloneExtensionMetadata(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(values))
	maps.Copy(cloned, values)

	return cloned
}

func textBufferState(name, text string) extension.BufferState {
	return extension.BufferState{
		Metadata: map[string]any{},
		Blocks:   []extension.BufferBlock{},
		Name:     name,
		Text:     text,
		Chars:    stringBufferChars(text),
		Label:    "",
		Cursor:   len([]rune(text)),
	}
}

func stringBufferChars(text string) []string {
	chars := make([]string, 0, len([]rune(text)))
	for _, char := range text {
		chars = append(chars, string(char))
	}

	return chars
}

func (app *App) extensionContext() map[string]any {
	return map[string]any{
		"mode":                 string(app.mode),
		"working":              app.working,
		"auth_working":         app.authWorking,
		"cwd":                  app.cwd,
		extensionDataSessionID: app.sessionID,
	}
}

func (app *App) applyExtensionEventResult(ctx context.Context, result *extension.TerminalEventResult) {
	if result == nil {
		return
	}
	for _, name := range result.DeletedBuffers {
		app.applyExtensionBufferDelete(name)
	}
	for _, name := range result.DeletedWindows {
		app.applyRuntimeWindowDelete(name)
	}
	for name, buffer := range result.Buffers {
		app.applyExtensionBuffer(name, &buffer)
	}
	for name := range result.Windows {
		window := result.Windows[name]
		app.applyRuntimeWindow(name, &window)
	}
	if result.Layout != nil {
		app.applyRuntimeLayout(result.Layout)
	}
	for _, action := range result.Actions {
		app.applyExtensionAction(ctx, action)
	}
	app.applyUIWindowResult(result)
}

func (app *App) applyExtensionAction(ctx context.Context, action extension.ActionCall) {
	handlers := map[string]func(){
		"autocomplete.accept": func() { _ = app.acceptAutocomplete() },
		"followup.dequeue":    app.dequeueFollowUp,
		"followup.queue":      app.queueFollowUp,
		"history.next":        func() { _ = app.showNextPrompt() },
		"history.prev":        func() { _ = app.showPreviousPrompt() },
		"interrupt":           func() { app.handleEscape(ctx) },
		"prompt.cancel":       func() { app.cancelActivePrompt(ctx) },
		"transcript.tree":     func() { app.openTreePanel(ctx) },
	}
	if handler, ok := handlers[action.Name]; ok {
		handler()
		return
	}
	if action.Name != "submit" {
		return
	}
	if _, err := app.submit(ctx); err != nil {
		app.addSystemMessage(err.Error())
	}
}

func (app *App) applyExtensionBuffer(name string, buffer *extension.BufferState) {
	switch name {
	case extensionBufferComposer:
		app.applyComposerBuffer(buffer)
	case extensionBufferStatus:
		app.extensionRuntimeBuffers[name] = cloneRuntimeBufferState(name, buffer)
		app.statusMessage = buffer.Text
	case extensionBufferTranscript, extensionBufferThinking, extensionBufferTools:
		app.extensionRuntimeBuffers[name] = cloneRuntimeBufferState(name, buffer)
	default:
		app.extensionRuntimeBuffers[name] = cloneRuntimeBufferState(name, buffer)
	}
}

func (app *App) applyRuntimeWindow(name string, window *extension.WindowState) {
	if window == nil {
		return
	}
	if window.Name == "" {
		window.Name = name
	}
	app.runtimeWindows[name] = *window
	app.ensureRuntimeLayoutWindow(name, window)
}

func (app *App) applyRuntimeLayout(layout *extension.LayoutState) {
	if layout == nil {
		return
	}
	cloned := extension.LayoutState{
		Windows: map[string]extension.WindowState{},
		Width:   layout.Width,
		Height:  layout.Height,
	}
	for name := range layout.Windows {
		window := layout.Windows[name]
		if window.Name == "" {
			window.Name = name
		}
		if window.Metadata == nil {
			window.Metadata = map[string]any{}
		}
		cloned.Windows[name] = window
	}
	app.runtimeLayout = &cloned
	app.runtimeWindows = map[string]extension.WindowState{}
	for name := range cloned.Windows {
		app.runtimeWindows[name] = cloned.Windows[name]
	}
}

func (app *App) ensureRuntimeLayoutWindow(name string, window *extension.WindowState) {
	if app.runtimeLayout == nil || window == nil {
		return
	}
	if app.runtimeLayout.Windows == nil {
		app.runtimeLayout.Windows = map[string]extension.WindowState{}
	}
	app.runtimeLayout.Windows[name] = *window
}

func (app *App) applyRuntimeWindowDelete(name string) {
	delete(app.runtimeWindows, name)
	if app.runtimeLayout != nil {
		delete(app.runtimeLayout.Windows, name)
	}
	delete(app.uiWindowOverrides, name)
	if app.uiCursor != nil && app.uiCursor.Window == name {
		app.uiCursor = nil
	}
}

func (app *App) applyExtensionBufferDelete(name string) {
	delete(app.extensionRuntimeBuffers, name)
	switch name {
	case extensionBufferComposer:
		app.setComposerBuffer(nil)
		app.resetPromptHistoryNavigation()
	case extensionBufferStatus:
		app.statusMessage = ""
	case extensionBufferTranscript:
		app.resetMessages()
	}
}

func (app *App) applyComposerBuffer(buffer *extension.BufferState) {
	oldText := app.composerText()
	oldCursor := app.composerCursor()
	app.setComposerBuffer(buffer)
	if app.composerText() != oldText || app.composerCursor() != oldCursor {
		app.resetPromptHistoryNavigation()
	}
}

func (app *App) applyUIWindowResult(result *extension.TerminalEventResult) {
	for _, windowName := range result.ResetUIWindows {
		app.resetUIWindowOverride(windowName)
	}
	for index := range result.UIDrawOps {
		app.appendUIWindowDrawOp(&result.UIDrawOps[index])
	}
	if result.UICursor != nil {
		cursor := *result.UICursor
		app.uiCursor = &cursor
	}
}

func (app *App) resetUIWindowOverride(name string) {
	if name == "" {
		return
	}
	override := app.uiWindowOverrides[name]
	override.Reset = true
	override.DrawOps = nil
	app.uiWindowOverrides[name] = override
	if app.uiCursor != nil && app.uiCursor.Window == name {
		app.uiCursor = nil
	}
}

func (app *App) appendUIWindowDrawOp(drawOp *extension.UIDrawOp) {
	if drawOp == nil || drawOp.Window == "" {
		return
	}
	override := app.uiWindowOverrides[drawOp.Window]
	override.DrawOps = append(override.DrawOps, *drawOp)
	app.uiWindowOverrides[drawOp.Window] = override
}

func terminalKeyEvent(event *tcell.EventKey) extension.ComposerKeyEvent {
	if keyEvent, ok := composerKeyEvent(event); ok {
		return keyEvent
	}

	keyName := strings.ToLower(event.Name())
	keyName = strings.ReplaceAll(keyName, "ctrl-", "ctrl+")
	keyName = strings.ReplaceAll(keyName, " ", "-")

	return extension.ComposerKeyEvent{
		Key:   keyName,
		Text:  event.Str(),
		Ctrl:  event.Modifiers()&tcell.ModCtrl != 0,
		Alt:   event.Modifiers()&tcell.ModAlt != 0,
		Shift: event.Modifiers()&tcell.ModShift != 0,
	}
}
