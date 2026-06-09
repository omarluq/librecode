package terminal

import (
	"context"
	"maps"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/terminal/input"
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

func (app *App) runRenderExtensions(ctx context.Context, layout *extui.Layout) {
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
		"message_count":         len(app.transcript.History),
		"streaming_block_count": len(app.transcript.Streaming.Blocks),
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
	app.extensionUI.ResetFrameOverrides()
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
	layout *extui.Layout,
	data map[string]any,
) extension.TerminalEvent {
	windows := app.cloneRuntimeWindows(layout)
	return extension.TerminalEvent{
		Buffers: app.extensionBuffers(),
		Windows: windows,
		Layout:  extension.LayoutState{Windows: windows, Width: layout.Width, Height: layout.Height},
		Context: app.extensionContext(),
		Data:    extui.CloneMetadata(data),
		Name:    name,
		Key:     key,
		Focus:   app.focusState(),
	}
}

func (app *App) extensionBuffers() map[string]extension.BufferState {
	reservedBuffers := app.reservedRuntimeBuffers()
	buffers := make(map[string]extension.BufferState, len(app.extensionUI.Buffers)+len(reservedBuffers))
	for name, buffer := range app.extensionUI.Buffers {
		buffers[name] = extui.CloneBuffer(name, &buffer)
	}
	maps.Copy(buffers, reservedBuffers)

	return buffers
}

func textBufferState(name, text string) extension.BufferState {
	return extension.BufferState{
		Metadata: map[string]any{},
		Blocks:   []extension.BufferBlock{},
		Name:     name,
		Text:     text,
		Chars:    input.StringChars(text),
		Label:    "",
		Cursor:   len([]rune(text)),
	}
}

func (app *App) extensionContext() map[string]any {
	return map[string]any{
		"mode":                 string(app.mode),
		"working":              app.working,
		"compacting":           app.compacting,
		"auth_working":         app.authWorking,
		"cwd":                  app.cwd,
		"focus":                app.focusState(),
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
	case extui.BufferComposer:
		app.applyComposerBuffer(buffer)
	case extui.BufferStatus:
		app.extensionUI.ApplyBuffer(name, buffer)
		app.statusMessage = buffer.Text
	case extui.BufferTranscript, extui.BufferThinking, extui.BufferTools:
		app.extensionUI.ApplyBuffer(name, buffer)
	default:
		app.extensionUI.ApplyBuffer(name, buffer)
	}
}

func (app *App) applyRuntimeWindow(name string, window *extension.WindowState) {
	app.extensionUI.ApplyWindow(name, window)
}

func (app *App) applyRuntimeLayout(layout *extension.LayoutState) {
	app.extensionUI.ApplyLayout(layout)
}

func (app *App) applyRuntimeWindowDelete(name string) {
	app.extensionUI.DeleteWindow(name)
}

func (app *App) applyExtensionBufferDelete(name string) {
	app.extensionUI.DeleteBuffer(name)
	switch name {
	case extui.BufferComposer:
		app.composerBuffer = input.NewBuffer()
		app.resetPromptHistoryNavigation()
	case extui.BufferStatus:
		app.statusMessage = ""
	case extui.BufferTranscript:
		app.resetMessages()
	}
}

func (app *App) applyComposerBuffer(buffer *extension.BufferState) {
	oldText := app.composerBuffer.TextValue()
	oldCursor := app.composerBuffer.CursorValue()
	app.composerBuffer = composerBufferFromExtension(buffer)
	if app.composerBuffer.TextValue() != oldText || app.composerBuffer.CursorValue() != oldCursor {
		app.resetPromptHistoryNavigation()
	}
	if app.composerBuffer.TextValue() != oldText {
		app.resetAutocompleteSelection()
	}
}

func (app *App) applyUIWindowResult(result *extension.TerminalEventResult) {
	for _, windowName := range result.ResetUIWindows {
		app.extensionUI.ResetWindowOverride(windowName)
	}
	for index := range result.UIDrawOps {
		app.extensionUI.AppendDrawOp(&result.UIDrawOps[index])
	}
	app.extensionUI.SetCursor(result.UICursor)
}
