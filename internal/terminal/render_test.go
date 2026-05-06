package terminal

import "testing"

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
	if app.statusMessage == "streaming response" {
		t.Fatal("response deltas should not set the streaming response status")
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
