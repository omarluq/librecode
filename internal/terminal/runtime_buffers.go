package terminal

import (
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"github.com/omarluq/librecode/internal/transcript"
)

const (
	maxTranscriptSnapshotBlocks    = 32
	maxTranscriptSnapshotTextRunes = 4_000
)

func (app *App) reservedRuntimeBuffers() map[string]extension.BufferState {
	return map[string]extension.BufferState{
		extui.BufferComposer:   extensionBufferFromComposer(app.composerBuffer),
		extui.BufferStatus:     app.statusBufferState(),
		extui.BufferTranscript: app.transcriptBufferState(),
		extui.BufferThinking:   app.thinkingBufferState(),
		extui.BufferTools:      app.toolsBufferState(),
	}
}

func (app *App) statusBufferState() extension.BufferState {
	if buffer, ok := app.runtimeBufferOverride(extui.BufferStatus); ok {
		return buffer
	}
	buffer := textBufferState(extui.BufferStatus, app.defaultStatusText())
	buffer.Metadata = map[string]any{extui.MetadataMessage: app.statusMessage}

	return buffer
}

func (app *App) transcriptBufferState() extension.BufferState {
	if buffer, ok := app.runtimeBufferOverride(extui.BufferTranscript); ok {
		return buffer
	}
	snapshot := app.transcriptBufferBlocks(maxTranscriptSnapshotBlocks)
	buffer := textBufferState(extui.BufferTranscript, "")
	buffer.Blocks = snapshot.Blocks
	buffer.Metadata = extui.CloneMetadata(snapshot.Metadata)
	buffer.Metadata[extui.MetadataCount] = len(app.transcript.History)
	buffer.Metadata["snapshot_count"] = snapshot.Count
	buffer.Metadata["snapshot_start"] = snapshot.Start
	buffer.Metadata["snapshot_limit"] = snapshot.Limit

	return buffer
}

func (app *App) thinkingBufferState() extension.BufferState {
	if buffer, ok := app.runtimeBufferOverride(extui.BufferThinking); ok {
		return buffer
	}
	buffer := textBufferState(extui.BufferThinking, "")
	buffer.Metadata = map[string]any{
		extui.MetadataCount: app.countRuntimeMessages(func(role transcript.Role) bool {
			return role == transcript.RoleThinking
		}),
	}

	return buffer
}

func (app *App) toolsBufferState() extension.BufferState {
	if buffer, ok := app.runtimeBufferOverride(extui.BufferTools); ok {
		return buffer
	}
	buffer := textBufferState(extui.BufferTools, "")
	buffer.Metadata = map[string]any{
		extui.MetadataCount: app.countRuntimeMessages(func(role transcript.Role) bool {
			return role == transcript.RoleToolResult || role == transcript.RoleBashExecution
		}),
	}

	return buffer
}

type transcriptBufferSnapshot struct {
	Metadata map[string]any
	Blocks   []extension.BufferBlock
	Count    int
	Start    int
	Limit    int
}

func (app *App) transcriptBufferBlocks(limit int) transcriptBufferSnapshot {
	count := len(app.transcript.History) + len(app.transcript.Streaming.Blocks)
	limit = clampTranscriptLimit(limit, count)
	start := max(0, count-limit)
	blocks := make([]extension.BufferBlock, 0, limit)
	for index := start; index < count; index++ {
		blocks = append(blocks, app.transcriptBlock(index))
	}

	return transcriptBufferSnapshot{
		Metadata: map[string]any{
			"message_count":   len(app.transcript.History),
			"queued_count":    len(app.queuedMessages),
			"streaming_count": len(app.transcript.Streaming.Blocks),
			"working":         app.working,
			"compacting":      app.compacting,
		},
		Blocks: blocks,
		Count:  count,
		Start:  start,
		Limit:  limit,
	}
}

func clampTranscriptLimit(limit, count int) int {
	if count <= 0 {
		return 0
	}
	if limit <= 0 || limit > maxTranscriptSnapshotBlocks {
		limit = maxTranscriptSnapshotBlocks
	}

	return min(limit, count)
}

func (app *App) transcriptBlock(index int) extension.BufferBlock {
	messageCount := len(app.transcript.History)
	if index < messageCount {
		return app.messageTranscriptBlock(index, app.transcript.History[index], false)
	}
	streamingIndex := index - messageCount

	return app.messageTranscriptBlock(index, app.transcript.Streaming.Blocks[streamingIndex], true)
}

func (app *App) messageTranscriptBlock(
	index int,
	message chatMessage,
	streaming bool,
) extension.BufferBlock {
	text, truncated := transcriptBlockText(message.Content)
	metadata := map[string]any{}
	if truncated {
		metadata["truncated"] = true
	}

	return extension.BufferBlock{
		Metadata:  metadata,
		CreatedAt: message.CreatedAt.Format(time.RFC3339Nano),
		ID:        transcriptBlockID(index, streaming),
		Kind:      transcriptBlockKind(streaming),
		Role:      string(message.Role),
		Text:      text,
		Index:     index,
		Streaming: streaming,
	}
}

func transcriptBlockText(text string) (string, bool) {
	runes := []rune(text)
	if len(runes) <= maxTranscriptSnapshotTextRunes {
		return text, false
	}

	return string(runes[:maxTranscriptSnapshotTextRunes]), true
}

func transcriptBlockID(index int, streaming bool) string {
	prefix := extui.MetadataMessage
	if streaming {
		prefix = "streaming"
	}

	return prefix + ":" + intText(index)
}

func transcriptBlockKind(streaming bool) string {
	if streaming {
		return "streaming"
	}

	return extui.MetadataMessage
}

func (app *App) countRuntimeMessages(matchesRole func(transcript.Role) bool) int {
	count := 0
	for _, message := range app.transcript.History {
		if matchesRole(message.Role) {
			count++
		}
	}
	for _, message := range app.transcript.Streaming.Blocks {
		if matchesRole(message.Role) {
			count++
		}
	}

	return count
}

func (app *App) runtimeBufferOverride(name string) (extension.BufferState, bool) {
	return app.extensionUI.RuntimeBuffer(name)
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
	if tokenText := app.tokenStatusText(); tokenText != "" {
		modelText += " • " + tokenText
	}

	return []string{pathLine, modelText}
}

func (app *App) drawRuntimeTextBuffer(
	window *extension.WindowState,
	buffer *extension.BufferState,
	style tcell.Style,
) {
	lines := app.renderBufferTextLines(window.Width, buffer.Text, style)
	for index, line := range app.visibleMessageLineGroups([][]rendertext.Line{lines}, window.Height) {
		app.writeStyledLine(window.Y+index, window.Width, line)
	}
}

func (app *App) renderBufferTextLines(width int, text string, style tcell.Style) []rendertext.Line {
	if text == "" {
		return []rendertext.Line{}
	}
	parts := strings.Split(text, "\n")
	lines := make([]rendertext.Line, 0, len(parts))
	for _, part := range parts {
		wrapped := rendertext.Wrap(part, width)
		if len(wrapped) == 0 {
			lines = append(lines, rendertext.NewLine(style, ""))
			continue
		}
		for _, line := range wrapped {
			lines = append(lines, rendertext.NewLine(style, line))
		}
	}

	return lines
}
