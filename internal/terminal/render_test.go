//nolint:testpackage // These tests exercise unexported terminal rendering helpers.
package terminal

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

func TestRenderStreamingMessageUsesTextColor(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	lines := app.renderStreamingMessage(80, "hello")
	if len(lines) < 2 {
		t.Fatalf("expected content line, got %d lines", len(lines))
	}

	content := lines[1]
	if got, want := content.Style.GetForeground(), app.theme.colors[colorText]; got != want {
		t.Fatalf("streaming response foreground = %v, want %v", got, want)
	}
	if content.Style.HasItalic() {
		t.Fatal("streaming response should not be italicized")
	}
}

func TestRenderThinkingMessageKeepsDimColor(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	lines := app.renderThinkingMessage(80, newChatMessage(database.RoleThinking, "thinking details"))
	assertThinkingLineDim(t, app, lines)
}

func TestRenderStreamingThinkingMessageKeepsDimColor(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	lines := app.renderStreamingThinkingMessage(80, "thinking details")
	assertThinkingLineDim(t, app, lines)
}

func TestPromptThinkingDeltaUsesSeparateStreamingBuffer(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.handlePromptStreamEvent(newTestAsyncEvent(asyncEventPromptThinkingDelta, "thinking"))
	app.handlePromptStreamEvent(newTestAsyncEvent(asyncEventPromptDelta, "answer"))

	got := streamingBlockRoles(app.streamingBlocks)
	want := []database.Role{database.RoleThinking, database.RoleAssistant}
	if !rolesEqual(got, want) {
		t.Fatalf("streaming block roles = %v, want %v", got, want)
	}
	if app.statusMessage == "streaming response" {
		t.Fatal("response deltas should not set the streaming response status")
	}
}

func TestPromptToolStartDoesNotSetStatus(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setStatus("ready")

	app.handlePromptStreamEvent(newTestAsyncEvent(asyncEventPromptToolStart, "bash"))

	if app.statusMessage != "ready" {
		t.Fatalf("status = %q, want ready", app.statusMessage)
	}
}

func TestScrollTranscriptDoesNotDrawImmediately(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.scrollTranscript(3)

	if app.scrollOffset != 3 {
		t.Fatalf("scroll offset = %d, want 3", app.scrollOffset)
	}
	if app.statusMessage != "" {
		t.Fatalf("status = %q, want empty", app.statusMessage)
	}
}

func TestHighVolumeStreamEventsDoNotForceImmediateDraw(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	for _, kind := range []asyncEventKind{
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
	} {
		event := tcell.NewEventInterrupt(newTestAsyncEvent(kind, ""))
		if app.shouldDrawImmediately(event) {
			t.Fatalf("%s should be frame-throttled", kind)
		}
	}
}

func TestPromptLifecycleEventsForceImmediateDraw(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	for _, kind := range []asyncEventKind{
		asyncEventPromptDone,
		asyncEventPromptError,
		asyncEventAuthDone,
	} {
		event := tcell.NewEventInterrupt(newTestAsyncEvent(kind, ""))
		if !app.shouldDrawImmediately(event) {
			t.Fatalf("%s should draw immediately", kind)
		}
	}
}

func TestMessageLineCacheInvalidatesForThinkingVisibility(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.addMessage(database.RoleThinking, "cached thought")

	visible := app.messageLines(80, 100)
	if lineIndexContaining(visible, "cached thought") == -1 {
		t.Fatalf("expected visible thinking content before hiding, got %v", lineTexts(visible))
	}

	app.hideThinking = true
	hidden := app.messageLines(80, 100)
	if lineIndexContaining(hidden, "cached thought") != -1 {
		t.Fatalf("expected hidden thinking content after hiding, got %v", lineTexts(hidden))
	}
	if lineIndexContaining(hidden, "thinking…") == -1 {
		t.Fatalf("expected thinking placeholder after hiding, got %v", lineTexts(hidden))
	}
}

func TestStreamingBlockCacheInvalidatesOnMergedDelta(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.handlePromptStreamEvent(newTestAsyncEvent(asyncEventPromptThinkingDelta, "first"))
	_ = app.messageLines(80, 100)
	app.handlePromptStreamEvent(newTestAsyncEvent(asyncEventPromptThinkingDelta, " second"))

	lines := app.messageLines(80, 100)
	if lineIndexContaining(lines, "first second") == -1 {
		t.Fatalf("expected merged thinking delta to invalidate cached streaming block, got %v", lineTexts(lines))
	}
}

func TestStreamingBlocksRenderChronologically(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.handlePromptStreamEvent(newTestAsyncEvent(asyncEventPromptThinkingDelta, "first thought"))
	toolEvent := newTestAsyncEvent(asyncEventPromptToolResult, "")
	toolEvent.ToolEvent = newTestToolEvent("read", "tool output")
	app.handlePromptStreamEvent(toolEvent)
	app.handlePromptStreamEvent(newTestAsyncEvent(asyncEventPromptThinkingDelta, "second thought"))

	got := streamingBlockRoles(app.streamingBlocks)
	want := []database.Role{
		database.RoleThinking,
		database.RoleToolResult,
		database.RoleThinking,
	}
	if !rolesEqual(got, want) {
		t.Fatalf("streaming block roles = %v, want %v", got, want)
	}

	lines := app.messageLines(80, 200)
	first := lineIndexContaining(lines, "first thought")
	tool := lineIndexContaining(lines, "read")
	second := lineIndexContaining(lines, "second thought")
	if first == -1 || tool == -1 || second == -1 {
		t.Fatalf("expected rendered thinking/tool/thinking lines, got first=%d tool=%d second=%d", first, tool, second)
	}
	if first >= tool || tool >= second {
		t.Fatalf("streaming lines not chronological: first=%d tool=%d second=%d", first, tool, second)
	}
}

func newRenderTestApp(t *testing.T) *App {
	t.Helper()

	app := newApp(nil, &RunOptions{
		Extensions: nil,
		Resources:  nil,
		Runtime:    nil,
		Settings:   nil,
		Models:     nil,
		Auth:       nil,
		Config:     nil,
		CWD:        "",
		SessionID:  "",
	})
	app.resetMessages()

	return app
}

func newTestAsyncEvent(kind asyncEventKind, text string) asyncEvent {
	return asyncEvent{
		Response:  nil,
		ToolEvent: nil,
		Kind:      kind,
		Provider:  "",
		Text:      text,
		PromptID:  0,
	}
}

func newTestToolEvent(name, result string) *assistant.ToolEvent {
	return &assistant.ToolEvent{
		Name:          name,
		ArgumentsJSON: "",
		DetailsJSON:   "",
		Result:        result,
		Error:         "",
	}
}

func assertThinkingLineDim(t *testing.T, app *App, lines []styledLine) {
	t.Helper()
	if len(lines) < 3 {
		t.Fatalf("expected thinking content line, got %d lines", len(lines))
	}

	content := lines[2]
	if got, want := content.Style.GetForeground(), app.theme.colors[colorDim]; got != want {
		t.Fatalf("thinking foreground = %v, want %v", got, want)
	}
	if !content.Style.HasItalic() {
		t.Fatal("thinking text should remain italicized/dim")
	}
}

func streamingBlockRoles(blocks []chatMessage) []database.Role {
	roles := make([]database.Role, 0, len(blocks))
	for _, block := range blocks {
		roles = append(roles, block.Role)
	}

	return roles
}

func rolesEqual(left, right []database.Role) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}

func lineIndexContaining(lines []styledLine, text string) int {
	for index, line := range lines {
		if strings.Contains(line.Text, text) {
			return index
		}
	}

	return -1
}

func lineTexts(lines []styledLine) []string {
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}

	return texts
}
