package terminal

import (
	"context"
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
)

const (
	extensionEventKey          = "key"
	extensionEventPromptSubmit = "prompt_submit"
	extensionEventStartup      = "startup"
	extensionBufferComposer    = "composer"
	extensionBufferStatus      = "status"
	extensionBufferTranscript  = "transcript"
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
	return extension.TerminalEvent{
		Buffers: app.extensionBuffers(),
		Windows: app.extensionWindows(),
		Context: app.extensionContext(),
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

func (app *App) extensionWindows() map[string]extension.WindowState {
	layout := app.composerLayout(80, 24)
	return map[string]extension.WindowState{
		extensionBufferComposer: {
			Metadata:  map[string]any{},
			Name:      extensionBufferComposer,
			Role:      extensionBufferComposer,
			Buffer:    extensionBufferComposer,
			X:         0,
			Y:         layout.editorStart,
			Width:     80,
			Height:    len(layout.editor.Lines),
			CursorRow: layout.editor.CursorRow,
			CursorCol: layout.editor.CursorCol,
			Visible:   true,
		},
		extensionBufferStatus: {
			Metadata:  map[string]any{},
			Name:      extensionBufferStatus,
			Role:      extensionBufferStatus,
			Buffer:    extensionBufferStatus,
			X:         0,
			Y:         layout.footerStart,
			Width:     80,
			Height:    len(layout.footerLines),
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
		extensionBufferTranscript: {
			Metadata:  map[string]any{"count": len(app.messages)},
			Name:      extensionBufferTranscript,
			Role:      extensionBufferTranscript,
			Buffer:    extensionBufferTranscript,
			X:         0,
			Y:         0,
			Width:     80,
			Height:    0,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
	}
}

func (app *App) extensionContext() map[string]any {
	return map[string]any{
		"mode":         string(app.mode),
		"working":      app.working,
		"auth_working": app.authWorking,
		"cwd":          app.cwd,
		"session_id":   app.sessionID,
	}
}

func (app *App) applyExtensionEventResult(ctx context.Context, result *extension.TerminalEventResult) {
	if result == nil {
		return
	}
	for _, name := range result.DeletedBuffers {
		app.applyExtensionBufferDelete(name)
	}
	for name, buffer := range result.Buffers {
		app.applyExtensionBuffer(name, &buffer)
	}
	for _, bufferAppend := range result.Appends {
		app.applyExtensionBufferAppend(bufferAppend)
	}
	for _, action := range result.Actions {
		app.applyExtensionAction(ctx, action)
	}
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
