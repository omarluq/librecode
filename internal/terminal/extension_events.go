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
	extensionBufferComposer    = "composer"
	extensionBufferStatus      = "status"
	extensionBufferTranscript  = "transcript"
)

func terminalEventRunner(runner extension.ComposerRunner) extension.TerminalEventRunner {
	terminalRunner, ok := runner.(extension.TerminalEventRunner)
	if !ok {
		return nil
	}

	return terminalRunner
}

func (app *App) handleExtensionKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if app.extensions == nil {
		return false, nil
	}

	result, err := app.extensions.HandleTerminalEvent(
		ctx,
		app.newExtensionEvent(extensionEventKey, terminalKeyEvent(event)),
	)
	if err != nil {
		return false, err
	}
	app.applyExtensionEventResult(result)

	return result.Consumed, nil
}

func (app *App) applyPromptSubmitExtensions(ctx context.Context) (bool, error) {
	if app.extensions == nil {
		return false, nil
	}

	result, err := app.extensions.HandleTerminalEvent(
		ctx,
		app.newExtensionEvent(extensionEventPromptSubmit, extension.ComposerKeyEvent{
			Key:  "enter",
			Text: "",
			Ctrl: false,
			Alt:  false,
		}),
	)
	if err != nil {
		return false, err
	}
	app.applyExtensionEventResult(result)

	return result.Consumed, nil
}

func (app *App) newExtensionEvent(name string, key extension.ComposerKeyEvent) extension.TerminalEvent {
	return extension.TerminalEvent{
		Buffers: app.extensionBuffers(),
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
	label := ""
	if app.composer != nil {
		label = app.composer.label
	}

	return extension.BufferState{
		Metadata: map[string]any{},
		Name:     extensionBufferComposer,
		Text:     app.editor.text(),
		Chars:    editorChars(app.editor.value),
		Label:    label,
		Cursor:   app.editor.cursor,
	}
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
	eventContext := map[string]any{
		"mode":         string(app.mode),
		"working":      app.working,
		"auth_working": app.authWorking,
		"cwd":          app.cwd,
		"session_id":   app.sessionID,
	}
	if app.composer != nil {
		eventContext["composer_mode"] = app.composer.mode
		eventContext["composer_label"] = app.composer.label
	}

	return eventContext
}

func (app *App) applyExtensionEventResult(result extension.TerminalEventResult) {
	for _, name := range result.DeletedBuffers {
		app.applyExtensionBufferDelete(name)
	}
	for name, buffer := range result.Buffers {
		app.applyExtensionBuffer(name, &buffer)
	}
	for _, bufferAppend := range result.Appends {
		app.applyExtensionBufferAppend(bufferAppend)
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
		app.editor.clear()
		app.resetPromptHistoryNavigation()
	case extensionBufferStatus:
		app.statusMessage = ""
	case extensionBufferTranscript:
		app.resetMessages()
	}
}

func (app *App) applyComposerBuffer(buffer *extension.BufferState) {
	oldText := app.editor.text()
	app.editor.setText(buffer.Text)
	app.editor.cursor = min(max(0, buffer.Cursor), len(app.editor.value))
	if app.composer != nil && buffer.Label != "" {
		app.composer.label = buffer.Label
	}
	if buffer.Text != oldText {
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
		Key:  keyName,
		Text: event.Str(),
		Ctrl: event.Modifiers()&tcell.ModCtrl != 0,
		Alt:  event.Modifiers()&tcell.ModAlt != 0,
	}
}
