package terminal

import (
	"context"
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
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
	extensionEventResize          = "resize"
	extensionEventStartup         = "startup"
	extensionEventThinkingDelta   = "thinking_delta"
	extensionEventToolEnd         = "tool_end"
	extensionEventToolStart       = "tool_start"
	extensionBufferComposer       = "composer"
	extensionBufferStatus         = "status"
	extensionBufferTranscript     = "transcript"
)

func (app *App) runStartupExtensions(ctx context.Context) error {
	if app.extensions == nil {
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
	if app.extensions == nil {
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
	layout := app.currentRuntimeLayout()

	return app.emitExtensionRuntimeEvent(ctx, extensionEventResize, map[string]any{
		extensionDataWidth:  layout.Width,
		extensionDataHeight: layout.Height,
	})
}

func (app *App) runRenderExtensions(ctx context.Context, layout *runtimeLayout) {
	if layout == nil {
		return
	}
	app.resetFrameUIOverrides()
	if app.extensions == nil {
		return
	}
	data := map[string]any{extensionDataWidth: layout.Width, extensionDataHeight: layout.Height}
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

func (app *App) emitExtensionRuntimeEvent(ctx context.Context, name string, data map[string]any) error {
	if app.extensions == nil {
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
	if app.extensions == nil {
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
	buffers := make(map[string]extension.BufferState, len(app.extensionRuntimeBuffers)+3)
	for name, buffer := range app.extensionRuntimeBuffers {
		buffers[name] = buffer
	}
	buffers[extensionBufferComposer] = app.composerBufferState()
	buffers[extensionBufferStatus] = textBufferState(extensionBufferStatus, app.statusMessage)
	buffers[extensionBufferTranscript] = extension.BufferState{
		Metadata: map[string]any{"count": len(app.messages)},
		Name:     extensionBufferTranscript,
		Text:     "",
		Chars:    []string{},
		Label:    "",
		Cursor:   0,
	}

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
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}

func textBufferState(name, text string) extension.BufferState {
	return extension.BufferState{
		Metadata: map[string]any{},
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
	for _, bufferAppend := range result.Appends {
		if app.resultBufferAlreadyApplied(&bufferAppend, result.Buffers) {
			continue
		}
		app.applyExtensionBufferAppend(bufferAppend)
	}
	for _, action := range result.Actions {
		app.applyExtensionAction(ctx, action)
	}
	app.applyUIWindowResult(result)
}

func (app *App) resultBufferAlreadyApplied(
	bufferAppend *extension.BufferAppend,
	buffers map[string]extension.BufferState,
) bool {
	if bufferAppend == nil || bufferAppend.Name == extensionBufferTranscript {
		return false
	}
	_, ok := buffers[bufferAppend.Name]

	return ok
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
		app.statusMessage = buffer.Text
	case extensionBufferTranscript:
		return
	default:
		app.extensionRuntimeBuffers[name] = *buffer
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

func (app *App) applyExtensionBufferAppend(bufferAppend extension.BufferAppend) {
	if bufferAppend.Name == extensionBufferTranscript {
		if strings.TrimSpace(bufferAppend.Text) == "" {
			return
		}
		app.addMessage(extensionAppendRole(bufferAppend.Role), bufferAppend.Text)
		return
	}

	buffer := app.extensionRuntimeBuffers[bufferAppend.Name]
	if buffer.Name == "" {
		buffer = textBufferState(bufferAppend.Name, "")
	}
	buffer.Text += bufferAppend.Text
	buffer.Chars = stringBufferChars(buffer.Text)
	buffer.Cursor = len([]rune(buffer.Text))
	app.extensionRuntimeBuffers[bufferAppend.Name] = buffer
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

func extensionAppendRole(role string) database.Role {
	switch role {
	case string(database.RoleUser):
		return database.RoleUser
	case string(database.RoleAssistant):
		return database.RoleAssistant
	case string(database.RoleThinking):
		return database.RoleThinking
	case string(database.RoleToolResult):
		return database.RoleToolResult
	case string(database.RoleBashExecution):
		return database.RoleBashExecution
	case string(database.RoleBranchSummary):
		return database.RoleBranchSummary
	case string(database.RoleCompactionSummary):
		return database.RoleCompactionSummary
	case string(database.RoleCustom), "system", "":
		return database.RoleCustom
	}

	return database.RoleCustom
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
