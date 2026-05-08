package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
)

const (
	extensionBufferThinking = "thinking"
	extensionBufferTools    = "tools"
	extensionMetadataCount  = "count"
)

func (app *App) reservedRuntimeBuffers() map[string]extension.BufferState {
	return map[string]extension.BufferState{
		extensionBufferComposer:   app.composerBufferState(),
		extensionBufferStatus:     app.statusBufferState(),
		extensionBufferTranscript: app.transcriptBufferState(),
		extensionBufferThinking:   app.thinkingBufferState(),
		extensionBufferTools:      app.toolsBufferState(),
	}
}

func (app *App) statusBufferState() extension.BufferState {
	if buffer, ok := app.runtimeBufferOverride(extensionBufferStatus); ok {
		return buffer
	}
	buffer := textBufferState(extensionBufferStatus, app.defaultStatusText())
	buffer.Metadata = map[string]any{"message": app.statusMessage}

	return buffer
}

func (app *App) transcriptBufferState() extension.BufferState {
	if buffer, ok := app.runtimeBufferOverride(extensionBufferTranscript); ok {
		return buffer
	}
	buffer := textBufferState(extensionBufferTranscript, "")
	buffer.Metadata = map[string]any{
		extensionMetadataCount: len(app.messages),
		"queued_count":         len(app.queuedMessages),
		"streaming_count":      len(app.streamingBlocks),
	}

	return buffer
}

func (app *App) thinkingBufferState() extension.BufferState {
	if buffer, ok := app.runtimeBufferOverride(extensionBufferThinking); ok {
		return buffer
	}
	buffer := textBufferState(extensionBufferThinking, "")
	buffer.Metadata = map[string]any{
		extensionMetadataCount: app.countRuntimeMessages(func(role database.Role) bool {
			return role == database.RoleThinking
		}),
	}

	return buffer
}

func (app *App) toolsBufferState() extension.BufferState {
	if buffer, ok := app.runtimeBufferOverride(extensionBufferTools); ok {
		return buffer
	}
	buffer := textBufferState(extensionBufferTools, "")
	buffer.Metadata = map[string]any{
		extensionMetadataCount: app.countRuntimeMessages(func(role database.Role) bool {
			return role == database.RoleToolResult || role == database.RoleBashExecution
		}),
	}

	return buffer
}

func (app *App) countRuntimeMessages(matchesRole func(database.Role) bool) int {
	count := 0
	for _, message := range app.messages {
		if matchesRole(message.Role) {
			count++
		}
	}
	for _, message := range app.streamingBlocks {
		if matchesRole(message.Role) {
			count++
		}
	}

	return count
}

func (app *App) runtimeBufferOverride(name string) (extension.BufferState, bool) {
	buffer, ok := app.extensionRuntimeBuffers[name]
	if !ok {
		var empty extension.BufferState

		return empty, false
	}

	return cloneRuntimeBufferState(name, &buffer), true
}

func (app *App) hasRuntimeBufferOverride(name string) bool {
	_, ok := app.extensionRuntimeBuffers[name]

	return ok
}

func cloneRuntimeBufferState(name string, buffer *extension.BufferState) extension.BufferState {
	cloned := *buffer
	if cloned.Name == "" {
		cloned.Name = name
	}
	cloned.Metadata = cloneExtensionMetadata(cloned.Metadata)
	cloned.Chars = append([]string{}, cloned.Chars...)
	if cloned.Chars == nil {
		cloned.Chars = stringBufferChars(cloned.Text)
	}
	cloned.Cursor = clampComposerCursor(cloned.Cursor, len([]rune(cloned.Text)))

	return cloned
}

func (app *App) defaultStatusText() string {
	return strings.Join(app.defaultStatusLineTexts(), "\n")
}

func (app *App) defaultStatusLineTexts() []string {
	pathLine := app.cwd
	if app.sessionID != "" {
		pathLine += " • " + app.sessionID
	}
	modelText := modelLabel(app.currentProvider(), app.currentModel())
	if app.currentThinkingLevel() != "" {
		modelText += " • " + app.currentThinkingLevel()
	}

	return []string{pathLine, modelText}
}

func (app *App) drawRuntimeTextBuffer(
	window *extension.WindowState,
	buffer *extension.BufferState,
	style tcell.Style,
) {
	lines := app.renderBufferTextLines(window.Width, buffer.Text, style)
	for index, line := range app.visibleMessageLineGroups([][]styledLine{lines}, window.Height) {
		app.writeStyledLine(window.Y+index, window.Width, line)
	}
}

func (app *App) renderBufferTextLines(width int, text string, style tcell.Style) []styledLine {
	if text == "" {
		return []styledLine{}
	}
	parts := strings.Split(text, "\n")
	lines := make([]styledLine, 0, len(parts))
	for _, part := range parts {
		wrapped := wrapText(part, width)
		if len(wrapped) == 0 {
			lines = append(lines, styledLine{Style: style, Text: ""})
			continue
		}
		for _, line := range wrapped {
			lines = append(lines, styledLine{Style: style, Text: line})
		}
	}

	return lines
}
