//nolint:testpackage // These tests exercise unexported terminal rendering helpers.
package terminal

import (
	"context"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
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

	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptThinkingDelta, "thinking"))
	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptDelta, "answer"))

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

	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptToolStart, "bash"))

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

	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptThinkingDelta, "first"))
	_ = app.messageLines(80, 100)
	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptThinkingDelta, " second"))

	lines := app.messageLines(80, 100)
	if lineIndexContaining(lines, "first second") == -1 {
		t.Fatalf("expected merged thinking delta to invalidate cached streaming block, got %v", lineTexts(lines))
	}
}

func TestStreamingBlocksRenderChronologically(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptThinkingDelta, "first thought"))
	toolEvent := newTestAsyncEvent(asyncEventPromptToolResult, "")
	toolEvent.ToolEvent = newTestToolEvent("read", "tool output")
	app.handlePromptStreamEvent(context.Background(), toolEvent)
	secondThought := newTestAsyncEvent(asyncEventPromptThinkingDelta, "second thought")
	app.handlePromptStreamEvent(context.Background(), secondThought)

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

func TestExtensionRendererSkipsDefaultComposerDraw(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setComposerText("host text")
	layout := app.defaultRuntimeLayout(40, 12)
	app.frame = newCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	window := layout.Composer
	window.Renderer = "extension"
	app.runtimeWindows[window.Name] = window

	app.drawComposerWindow(&layout)

	if got := frameText(app.frame); strings.Contains(got, "host text") {
		t.Fatalf("extension-rendered composer should skip host draw, frame = %q", got)
	}
}

func TestUIRenderDrawsSpansWithoutClearingLaterSpans(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	layout := app.defaultRuntimeLayout(40, 12)
	app.frame = newCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	app.uiWindowOverrides[extensionBufferComposer] = uiWindowOverride{
		DrawOps: []extension.UIDrawOp{
			{
				Style: extension.UIStyle{FG: "", BG: "", Bold: false, Italic: false},
				Spans: []extension.UISpan{
					{Text: "hot", Style: extension.UIStyle{FG: "accent", BG: "", Bold: true, Italic: false}},
					{Text: " cold", Style: extension.UIStyle{FG: string(colorDim), BG: "", Bold: false, Italic: false}},
				},
				Window: extensionBufferComposer,
				Kind:   extension.UIDrawKindSpans,
				Text:   "",
				Row:    1,
				Col:    0,
				Width:  0,
				Height: 0,
				Clear:  false,
			},
		},
		Reset: true,
	}

	app.applyUIOverrides(&layout)

	text := frameText(app.frame)
	if !strings.Contains(text, "hot cold") {
		t.Fatalf("expected span text, frame = %q", text)
	}
	first := app.frame.cell(layout.Composer.X, layout.Composer.Y+1)
	second := app.frame.cell(layout.Composer.X+4, layout.Composer.Y+1)
	if got, want := first.Style.GetForeground(), app.theme.colors[colorAccent]; got != want {
		t.Fatalf("first span foreground = %v, want %v", got, want)
	}
	if got, want := second.Style.GetForeground(), app.theme.colors[colorDim]; got != want {
		t.Fatalf("second span foreground = %v, want %v", got, want)
	}
}

func TestUIRenderDrawsWideRunesByCellWidth(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	layout := app.defaultRuntimeLayout(40, 12)
	app.frame = newCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	app.uiWindowOverrides[extensionBufferComposer] = uiWindowOverride{
		DrawOps: []extension.UIDrawOp{
			{
				Style:  extension.UIStyle{FG: "text", BG: "", Bold: false, Italic: false},
				Spans:  []extension.UISpan{},
				Window: extensionBufferComposer,
				Kind:   extension.UIDrawKindText,
				Text:   "語x",
				Row:    1,
				Col:    0,
				Width:  0,
				Height: 0,
				Clear:  false,
			},
		},
		Reset: true,
	}

	app.applyUIOverrides(&layout)

	row := layout.Composer.Y + 1
	if got, want := app.frame.cell(layout.Composer.X, row).Rune, '語'; got != want {
		t.Fatalf("first cell = %q, want %q", got, want)
	}
	if got, want := app.frame.cell(layout.Composer.X+1, row).Rune, ' '; got != want {
		t.Fatalf("wide continuation cell = %q, want space", got)
	}
	if got, want := app.frame.cell(layout.Composer.X+2, row).Rune, 'x'; got != want {
		t.Fatalf("third cell = %q, want %q", got, want)
	}
}

func TestUIRenderClearRegionClipsToWindow(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	layout := app.defaultRuntimeLayout(40, 12)
	app.frame = newCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	for row := 0; row < layout.Composer.Height; row++ {
		writeTextAt(
			app.frame,
			layout.Composer.X,
			layout.Composer.Y+row,
			layout.Composer.Width,
			strings.Repeat("x", layout.Composer.Width),
			tcell.StyleDefault.Foreground(cellcolor.Red),
		)
	}
	app.uiWindowOverrides[extensionBufferComposer] = uiWindowOverride{
		DrawOps: []extension.UIDrawOp{
			{
				Style:  extension.UIStyle{FG: string(colorDim), BG: "", Bold: false, Italic: false},
				Spans:  []extension.UISpan{},
				Window: extensionBufferComposer,
				Kind:   extension.UIDrawKindClear,
				Text:   "",
				Row:    1,
				Col:    2,
				Width:  4,
				Height: 2,
				Clear:  true,
			},
		},
		Reset: false,
	}

	app.applyUIOverrides(&layout)

	cleared := app.frame.cell(layout.Composer.X+2, layout.Composer.Y+1)
	untouched := app.frame.cell(layout.Composer.X+1, layout.Composer.Y+1)
	if got, want := cleared.Rune, ' '; got != want {
		t.Fatalf("cleared cell = %q, want space", got)
	}
	if got, want := cleared.Style.GetForeground(), app.theme.colors[colorDim]; got != want {
		t.Fatalf("cleared foreground = %v, want %v", got, want)
	}
	if got, want := untouched.Rune, 'x'; got != want {
		t.Fatalf("untouched cell = %q, want x", got)
	}
	if got, want := untouched.Style.GetForeground(), cellcolor.Red; got != want {
		t.Fatalf("untouched foreground = %v, want %v", got, want)
	}
}

func TestDefaultLayoutComposerTouchesStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prompt string
	}{
		{name: "empty prompt", prompt: ""},
		{name: "multiline prompt", prompt: "one\ntwo\nthree"},
		{name: "wrapped prompt", prompt: strings.Repeat("wrapped ", 20)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.setComposerText(tt.prompt)

			layout := app.defaultRuntimeLayout(80, 24)
			composerBottom := layout.Composer.Y + layout.Composer.Height
			if composerBottom != layout.Status.Y {
				t.Fatalf("composer bottom = %d, status y = %d", composerBottom, layout.Status.Y)
			}
		})
	}
}

func frameText(frame *cellBuffer) string {
	if frame == nil {
		return ""
	}
	var builder strings.Builder
	for row := range frame.height {
		for column := range frame.width {
			builder.WriteRune(frame.cell(column, row).Rune)
		}
		builder.WriteRune('\n')
	}

	return builder.String()
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
