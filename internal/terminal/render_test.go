package terminal

import (
	"strings"
	"testing"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

func TestRenderStreamingMessageUsesTextColor(t *testing.T) {
	app := &App{theme: darkTheme()}

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
	app := &App{theme: darkTheme()}

	lines := app.renderThinkingMessage(80, chatMessage{Content: "thinking details"})
	assertThinkingLineDim(t, app, lines)
}

func TestRenderStreamingThinkingMessageKeepsDimColor(t *testing.T) {
	app := &App{theme: darkTheme()}

	lines := app.renderStreamingThinkingMessage(80, "thinking details")
	assertThinkingLineDim(t, app, lines)
}

func TestPromptThinkingDeltaUsesSeparateStreamingBuffer(t *testing.T) {
	app := &App{}

	app.handlePromptStreamEvent(asyncEvent{Kind: asyncEventPromptThinkingDelta, Text: "thinking"})
	app.handlePromptStreamEvent(asyncEvent{Kind: asyncEventPromptDelta, Text: "answer"})

	if app.streamingThinkingText != "thinking" {
		t.Fatalf("streaming thinking text = %q, want %q", app.streamingThinkingText, "thinking")
	}
	if app.streamingText != "answer" {
		t.Fatalf("streaming response text = %q, want %q", app.streamingText, "answer")
	}
	if got, want := streamingBlockRoles(app.streamingBlocks), []database.Role{database.RoleThinking, database.RoleAssistant}; !rolesEqual(got, want) {
		t.Fatalf("streaming block roles = %v, want %v", got, want)
	}
	if app.statusMessage == "streaming response" {
		t.Fatal("response deltas should not set the streaming response status")
	}
}

func TestStreamingBlocksRenderChronologically(t *testing.T) {
	app := &App{theme: darkTheme(), keys: newDefaultKeybindings()}

	app.handlePromptStreamEvent(asyncEvent{Kind: asyncEventPromptThinkingDelta, Text: "first thought"})
	app.handlePromptStreamEvent(asyncEvent{
		Kind: asyncEventPromptToolResult,
		ToolEvent: &assistant.ToolEvent{
			Name:   "read",
			Result: "tool output",
		},
	})
	app.handlePromptStreamEvent(asyncEvent{Kind: asyncEventPromptThinkingDelta, Text: "second thought"})

	if got, want := streamingBlockRoles(app.streamingBlocks), []database.Role{
		database.RoleThinking,
		database.RoleToolResult,
		database.RoleThinking,
	}; !rolesEqual(got, want) {
		t.Fatalf("streaming block roles = %v, want %v", got, want)
	}

	lines := app.messageLines(80, 200)
	first := lineIndexContaining(lines, "first thought")
	tool := lineIndexContaining(lines, "read")
	second := lineIndexContaining(lines, "second thought")
	if first == -1 || tool == -1 || second == -1 {
		t.Fatalf("expected rendered thinking/tool/thinking lines, got first=%d tool=%d second=%d", first, tool, second)
	}
	if !(first < tool && tool < second) {
		t.Fatalf("streaming lines not chronological: first=%d tool=%d second=%d", first, tool, second)
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
