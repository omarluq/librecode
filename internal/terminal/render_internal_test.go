package terminal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/transcript"
	"github.com/omarluq/librecode/internal/tui"
)

func TestClearWindowRespectsWindowOrigin(t *testing.T) {
	t.Parallel()

	buffer := tui.NewCellBuffer(5, 2, tcell.StyleDefault)
	for row := 0; row < buffer.Height(); row++ {
		for column := 0; column < buffer.Width(); column++ {
			buffer.SetContent(column, row, 'x', nil, tcell.StyleDefault)
		}
	}

	window := extension.WindowState{
		Metadata:  nil,
		Name:      "window",
		Role:      "",
		Buffer:    "",
		Renderer:  "",
		X:         2,
		Y:         0,
		Width:     2,
		Height:    2,
		CursorRow: 0,
		CursorCol: 0,
		Visible:   true,
	}
	clearWindow(buffer, &window)

	assert.Equal(t, 'x', buffer.Cell(1, 0).Rune)
	assert.Equal(t, ' ', buffer.Cell(2, 0).Rune)
	assert.Equal(t, ' ', buffer.Cell(3, 1).Rune)
	assert.Equal(t, 'x', buffer.Cell(4, 1).Rune)
}

func TestAllMessageLinesFlattensStaticAndDynamicGroups(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.transcript.History = []chatMessage{newChatMessage(transcript.RoleAssistant, "hello")}
	dynamic := [][]tui.Line{{tui.NewLine(app.theme.style(colorText), "dynamic")}}

	lines := app.allMessageLines(40, dynamic)
	texts := lineTexts(lines)
	assert.Contains(t, texts, " hello")
	assert.Contains(t, texts, "dynamic")
}

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

	lines := app.renderThinkingMessage(80, newChatMessage(transcript.RoleThinking, "thinking details"))
	assertThinkingLineDim(t, app, lines)
}

func TestRenderThinkingMessagePreservesMarkdownSpans(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderThinkingMessage(80, newChatMessage(transcript.RoleThinking, "```go\nfunc hi() {}\n```"))
	require.GreaterOrEqual(t, len(lines), 3, "expected thinking content line")
	content := lines[2]
	require.NotEmpty(t, content.Spans)
	assert.True(t, content.Style.HasItalic())

	for _, span := range content.Spans {
		assert.True(t, span.Style.HasItalic())
	}
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

	got := streamingBlockRoles(app.transcript.Streaming.Blocks)

	want := []transcript.Role{transcript.RoleThinking, transcript.RoleAssistant}
	if !rolesEqual(got, want) {
		t.Fatalf("streaming block roles = %v, want %v", got, want)
	}

	if app.statusMessage == "streaming response" {
		t.Fatal("response deltas should not set the streaming response status")
	}
}

func TestApplyPromptErrorKeepsStreamedProgressVisible(t *testing.T) {
	t.Parallel()

	app := newPromptErrorProgressTestApp(t)

	app.applyPromptError(context.Background(), "provider returned an empty response", app.activePrompt.ID)

	assertPromptErrorMessages(t, app, true)
	assert.Empty(t, app.transcript.Streaming.Blocks)
}

func TestApplyPromptErrorFinalizesCanceledProgressWithoutErrorMessage(t *testing.T) {
	t.Parallel()

	app := newPromptErrorProgressTestApp(t)
	app.activePrompt.Canceled = true

	app.applyPromptError(context.Background(), "context canceled", app.activePrompt.ID)

	assertPromptErrorMessages(t, app, false)
	assert.Empty(t, app.transcript.Streaming.Blocks)
	assert.Equal(t, "response canceled; progress saved", app.statusMessage)
}

func newPromptErrorProgressTestApp(t *testing.T) *App {
	t.Helper()

	app := newRenderTestApp(t)
	app.working = true
	app.activePrompt = newTestActivePrompt(nil)
	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptDelta, "partial"))
	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptThinkingDelta, "\n\n"))
	app.transcript.Streaming.Blocks = append(app.transcript.Streaming.Blocks,
		newChatMessage(transcript.RoleUser, "ignored user echo"),
		newChatMessage(transcript.RoleBashExecution, "bash output"),
		newChatMessage(transcript.RoleCustom, "custom progress"),
		newChatMessage(transcript.RoleBranchSummary, "ignored branch"),
		newChatMessage(transcript.RoleCompactionSummary, "ignored compaction"),
	)
	toolEvent := newTestAsyncEvent(asyncEventPromptToolResult, "")
	toolEvent.ToolEvent = newTestToolEvent("read", "file content")
	app.handlePromptStreamEvent(context.Background(), toolEvent)

	return app
}

func assertPromptErrorMessages(t *testing.T, app *App, wantErrorMessage bool) {
	t.Helper()

	assert.False(t, app.working)

	wantLen := 4
	if wantErrorMessage {
		wantLen = 5
	}

	require.Len(t, app.transcript.History, wantLen)
	assert.Equal(t, transcript.RoleAssistant, app.transcript.History[0].Role)
	assert.Equal(t, "partial", app.transcript.History[0].Content)
	assert.Equal(t, transcript.RoleBashExecution, app.transcript.History[1].Role)
	assert.Equal(t, "bash output", app.transcript.History[1].Content)
	assert.Equal(t, transcript.RoleCustom, app.transcript.History[2].Role)
	assert.Equal(t, "custom progress", app.transcript.History[2].Content)
	assert.Equal(t, transcript.RoleToolResult, app.transcript.History[3].Role)
	assert.Contains(t, app.transcript.History[3].Content, "tool: read")

	if wantErrorMessage {
		assert.Equal(t, transcript.RoleCustom, app.transcript.History[4].Role)
		assert.Equal(t, "provider returned an empty response", app.transcript.History[4].Content)
	}
}

func TestFooterLinesIgnoreTransientStatus(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setStatus("running tool: bash")

	lines := app.footerLines(120)
	for _, line := range lines {
		if strings.Contains(line.Text, "running tool") {
			t.Fatalf("footer rendered transient status line %q", line.Text)
		}
	}
}

func TestRenderQueuedMessagesRendersHeadersAndBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		queuedMessages []string
		name           string
		expectedLines  []string
		width          int
	}{
		{
			name:           "single queued message",
			queuedMessages: []string{"first queued"},
			width:          40,
			expectedLines: []string{
				"  queued follow-up 1                    ",
				"  first queued                          ",
			},
		},
		{
			name:           "multiple queued messages",
			queuedMessages: []string{"first queued", "second queued"},
			width:          40,
			expectedLines: []string{
				"  queued follow-up 1                    ",
				"  first queued                          ",
				"  queued follow-up 2                    ",
				"  second queued                         ",
			},
		},
	}

	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.queuedMessages = testCase.queuedMessages

			lines := app.renderQueuedMessages(testCase.width)

			texts := lineTexts(lines)
			for _, expected := range testCase.expectedLines {
				assert.Contains(t, texts, expected)
			}
		})
	}
}

func TestRenderBoxedMessagesUseBoxedLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		render   func(app *App) []tui.Line
		expected string
	}{
		{
			name: "custom message",
			render: func(app *App) []tui.Line {
				return app.renderCustomMessage(30, "system note")
			},
			expected: "  [system]                    ",
		},
		{
			name: "summary message",
			render: func(app *App) []tui.Line {
				return app.renderSummaryMessage(30, newChatMessage(transcript.RoleCompactionSummary, "summary note"))
			},
			expected: "  [compactionSummary]         ",
		},
	}

	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			assert.Contains(t, lineTexts(testCase.render(app)), testCase.expected)
		})
	}
}

func TestVisibleMessageLineGroupsDoesNotMutateScrollOffset(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.scrollOffset = 5
	groups := [][]tui.Line{{tui.NewLine(app.theme.style(colorText), "one")}}

	lines := app.visibleMessageLineGroups(groups, 10)

	require.Len(t, lines, 1)
	assert.Equal(t, 5, app.scrollOffset)
}

func TestRenderWelcomeMessageHasCardPadding(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderWelcomeMessage(90, welcomeMessagePrefix+strings.Join(welcomeBodyLines("/tmp/work"), "\n"))

	art := welcomeArt()
	if len(lines) < len(art)+2 {
		t.Fatalf("welcome lines = %d, want at least %d", len(lines), len(art)+2)
	}

	if strings.TrimSpace(lines[0].Text) != "" || strings.TrimSpace(lines[len(lines)-1].Text) != "" {
		t.Fatalf("welcome should have vertical padding, got first=%q last=%q", lines[0].Text, lines[len(lines)-1].Text)
	}

	artLine := lines[1].Text
	if !strings.Contains(artLine, "██╗") {
		t.Fatalf("welcome art should render logo content, got %q", artLine)
	}

	if got, want := tui.Width(artLine), 90; got != want {
		t.Fatalf("welcome line width = %d, want %d", got, want)
	}

	leftPadding := len(artLine) - len(strings.TrimLeft(artLine, " "))
	if leftPadding <= welcomePaddingX {
		t.Fatalf("welcome art should be centered within padded card, got left padding %d in %q", leftPadding, artLine)
	}
}

func TestRenderWorkingIndicatorHasMarginAndShimmer(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.workFrame = 0

	lines := app.renderWorkingIndicator(40)
	if len(lines) != 3 {
		t.Fatalf("working indicator lines = %d, want 3", len(lines))
	}

	if lines[0].Text != "" || lines[2].Text != "" {
		t.Fatalf("working indicator should have vertical margin, got %#v", lineTexts(lines))
	}

	if !isWorkingIndicatorText(lines[1].Text) {
		t.Fatal("working indicator line should opt into shimmer rendering")
	}

	if !strings.HasPrefix(lines[1].Text, "⠋ Shenaniganing...") {
		t.Fatalf("working indicator should align with page text: %q", lines[1].Text)
	}

	assertWorkingShimmerMotion(t, lines[1].Text)
}

func TestRenderCompactingIndicatorUsesCompactionTextAndPalette(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.workFrame = 0

	lines := app.renderWorkingIndicator(40)
	if len(lines) != 3 {
		t.Fatalf("compacting indicator lines = %d, want 3", len(lines))
	}

	if !strings.HasPrefix(lines[1].Text, "⠋ Compacting context...") {
		t.Fatalf("compacting indicator text = %q", lines[1].Text)
	}

	palette := app.workingShimmerPalette()
	if got, want := palette.bright, compactingShimmerBrightColor(); got != want {
		t.Fatalf("compacting shimmer bright = %v, want %v", got, want)
	}

	app.frame = tui.NewCellBuffer(40, len(lines), tcell.StyleDefault)
	app.writeStyledLine(1, 40, lines[1])

	if got, want := app.frame.Cell(0, 1).Style.GetForeground(), palette.bright; got != want {
		t.Fatalf("spinner foreground = %v, want %v", got, want)
	}

	_, contentWidth := workingShimmerContentRange(lines[1].Text)
	labelForeground := app.frame.Cell(2, 1).Style.GetForeground()

	labelShimmer := workingShimmerColor(0, 0, contentWidth, palette)
	if labelForeground != labelShimmer {
		t.Fatalf("label foreground = %v, want %v", labelForeground, labelShimmer)
	}
}

func assertWorkingShimmerMotion(t *testing.T, indicatorText string) {
	t.Helper()

	palette := defaultWorkingShimmerPalette()
	bright := palette.bright
	base := palette.base
	_, contentWidth := workingShimmerContentRange(indicatorText)

	checks := []struct {
		name     string
		position int
		column   int
		want     tcell.Color
	}{
		{name: "starts at left", position: 0, column: 0, want: bright},
		{name: "distant cell stays base", position: 0, column: 6, want: base},
		{name: "moves right", position: 3, column: 3, want: bright},
		{name: "leaves bright trail behind head", position: 3, column: 2, want: hexColorFromString("#a8f2e9")},
		{name: "reaches final cell", position: contentWidth - 1, column: contentWidth - 1, want: bright},
		{name: "does not wrap trail", position: contentWidth - 1, column: 0, want: base},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			t.Parallel()

			if got := workingShimmerColor(check.position, check.column, contentWidth, palette); got != check.want {
				t.Fatalf("color = %v, want %v", got, check.want)
			}
		})
	}
}

func TestWorkingShimmerUsesSweepDuration(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	contentWidth := len("Shenaniganing...")
	app.workStartedAt = time.Now().Add(-loaderShimmerSweepDuration / 2)
	got := app.workingShimmerPosition(contentWidth)
	wantMin := contentWidth/2 - 1

	wantMax := contentWidth/2 + 1
	if got < wantMin || got > wantMax {
		t.Fatalf("shimmer position = %d, want between %d and %d", got, wantMin, wantMax)
	}
}

func TestWriteStyledLineOnlyShimmersMarkedLines(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.frame = tui.NewCellBuffer(20, 2, tcell.StyleDefault)
	app.workFrame = 0
	row := 0
	app.writeStyledLine(row, 20, tui.NewLine(app.theme.style(colorText), "assistant working… text"))

	if got, want := app.frame.Cell(0, row).Style.GetForeground(), app.theme.colors[colorText]; got != want {
		t.Fatalf("plain line foreground = %v, want %v", got, want)
	}

	row = 1
	app.writeStyledLine(row, 20, tui.NewLine(app.theme.style(colorText), "⠋ Shenaniganing..."))

	if got, want := app.frame.Cell(0, row).Style.GetForeground(), defaultWorkingShimmerBrightColor(); got != want {
		t.Fatalf("spinner foreground = %v, want %v", got, want)
	}

	textStart := 2
	got := app.frame.Cell(textStart, row).Style.GetForeground()

	want := workingShimmerColor(0, 0, len("Shenaniganing..."), defaultWorkingShimmerPalette())
	if got != want {
		t.Fatalf("shimmer text foreground = %v, want %v", got, want)
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

func TestHandleTranscriptScroll(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		event      *tcell.EventKey
		name       string
		wantOffset int
		wantOK     bool
	}{
		{
			name:       "page up scrolls up",
			event:      tcell.NewEventKey(tcell.KeyPgUp, "", tcell.ModNone),
			wantOffset: keyboardScrollRows,
			wantOK:     true,
		},
		{
			name:       "page down scrolls down but clamps at top",
			event:      tcell.NewEventKey(tcell.KeyPgDn, "", tcell.ModNone),
			wantOffset: 0,
			wantOK:     true,
		},
		{
			name:       "non-scroll key is ignored",
			event:      tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone),
			wantOffset: 0,
			wantOK:     false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)

			ok := app.handleTranscriptScroll(testCase.event)

			assert.Equal(t, testCase.wantOK, ok)
			assert.Equal(t, testCase.wantOffset, app.scrollOffset)
		})
	}
}

func TestMouseWheelScrollsTranscript(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.addMessage(transcript.RoleAssistant, "scrollable content")

	app.handleMouse(tcell.NewEventMouse(0, 0, tcell.WheelUp, tcell.ModNone))

	assert.Equal(t, mouseScrollRows, app.scrollOffset)
}

func TestScrollDeltaForEvent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		event     tcell.Event
		name      string
		mode      appMode
		wantDelta int
		wantOK    bool
	}{
		{
			name:      "mouse wheel up in chat mode",
			event:     tcell.NewEventMouse(0, 0, tcell.WheelUp, tcell.ModNone),
			mode:      modeChat,
			wantDelta: mouseScrollRows,
			wantOK:    true,
		},
		{
			name:      "mouse wheel down in chat mode",
			event:     tcell.NewEventMouse(0, 0, tcell.WheelDown, tcell.ModNone),
			mode:      modeChat,
			wantDelta: -mouseScrollRows,
			wantOK:    true,
		},
		{
			name:      "page up key in chat mode",
			event:     tcell.NewEventKey(tcell.KeyPgUp, "", tcell.ModNone),
			mode:      modeChat,
			wantDelta: keyboardScrollRows,
			wantOK:    true,
		},
		{
			name:      "page down key in chat mode",
			event:     tcell.NewEventKey(tcell.KeyPgDn, "", tcell.ModNone),
			mode:      modeChat,
			wantDelta: -keyboardScrollRows,
			wantOK:    true,
		},
		{
			name:      "non-scroll key in chat mode",
			event:     tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone),
			mode:      modeChat,
			wantDelta: 0,
			wantOK:    false,
		},
		{
			name:      "mouse wheel outside chat mode",
			event:     tcell.NewEventMouse(0, 0, tcell.WheelUp, tcell.ModNone),
			mode:      modePanel,
			wantDelta: 0,
			wantOK:    false,
		},
		{
			name:      "page up key outside chat mode",
			event:     tcell.NewEventKey(tcell.KeyPgUp, "", tcell.ModNone),
			mode:      modePanel,
			wantDelta: 0,
			wantOK:    false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.mode = testCase.mode

			delta, ok := app.scrollDeltaForEvent(testCase.event)

			assert.Equal(t, testCase.wantDelta, delta)
			assert.Equal(t, testCase.wantOK, ok)
		})
	}
}

func TestScrollLoopEventCoalescesQueuedScrolls(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		wantComposer string
		queuedEvents []tcell.Event
		wantOffset   int
		wantDirty    bool
	}{
		{
			name:         "folds queued scroll events into one draw",
			wantComposer: "",
			queuedEvents: []tcell.Event{
				tcell.NewEventMouse(0, 0, tcell.WheelUp, tcell.ModNone),
				tcell.NewEventMouse(0, 0, tcell.WheelDown, tcell.ModNone),
			},
			wantOffset: mouseScrollRows,
			wantDirty:  false,
		},
		{
			name:         "stops at first non-scroll event and handles it",
			wantComposer: "x",
			queuedEvents: []tcell.Event{
				tcell.NewEventMouse(0, 0, tcell.WheelUp, tcell.ModNone),
				tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone),
			},
			wantOffset: mouseScrollRows * 2,
			wantDirty:  false,
		},
		{
			name:         "propagates pending event dirty state",
			wantComposer: "",
			queuedEvents: []tcell.Event{
				tcell.NewEventInterrupt(asyncTestEvent(asyncEventPromptDelta, "", "chunk", 1)),
			},
			wantOffset: mouseScrollRows,
			wantDirty:  true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			must := require.New(t)
			app := newScrollableRenderTestApp(t)
			app.activePrompt = newTestActivePrompt(nil)
			app.activePrompt.ID = 1

			events := app.screen.EventQ()
			for _, event := range testCase.queuedEvents {
				events <- event
			}

			shouldQuit, dirty := app.handleScrollLoopEvent(context.Background(), mouseScrollRows)

			must.False(shouldQuit)
			assert.Equal(t, testCase.wantDirty, dirty)
			assert.Equal(t, testCase.wantOffset, app.scrollOffset)
			assert.Equal(t, testCase.wantComposer, app.composerBuffer.TextValue())
		})
	}
}

func TestRunLoopStepHandlesQueuedEvents(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		event        tcell.Event
		wantComposer string
		wantOffset   int
		wantQuit     bool
		wantDirty    bool
	}{
		{
			name:         "nil event quits",
			event:        nil,
			wantComposer: "",
			wantOffset:   0,
			wantQuit:     true,
			wantDirty:    false,
		},
		{
			name:         "key event draws immediately",
			event:        tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone),
			wantComposer: "x",
			wantOffset:   0,
			wantQuit:     false,
			wantDirty:    false,
		},
		{
			name:         "high-volume async event returns dirty",
			event:        tcell.NewEventInterrupt(asyncTestEvent(asyncEventPromptDelta, "", "chunk", 1)),
			wantComposer: "",
			wantOffset:   0,
			wantQuit:     false,
			wantDirty:    true,
		},
		{
			name:         "scroll event draws immediately",
			event:        tcell.NewEventMouse(0, 0, tcell.WheelUp, tcell.ModNone),
			wantComposer: "",
			wantOffset:   mouseScrollRows,
			wantQuit:     false,
			wantDirty:    false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newScrollableRenderTestApp(t)
			app.activePrompt = newTestActivePrompt(nil)
			app.activePrompt.ID = 1

			app.screen.EventQ() <- testCase.event

			shouldQuit, dirty := runLoopStepWithDormantTimers(t, app)

			assert.Equal(t, testCase.wantQuit, shouldQuit)
			assert.Equal(t, testCase.wantDirty, dirty)
			assert.Equal(t, testCase.wantOffset, app.scrollOffset)
			assert.Equal(t, testCase.wantComposer, app.composerBuffer.TextValue())
		})
	}
}

func runLoopStepWithDormantTimers(t *testing.T, app *App) (shouldQuit, dirty bool) {
	t.Helper()

	workTicker := time.NewTicker(time.Hour)
	frameTicker := time.NewTicker(time.Hour)
	extensionTimer := time.NewTimer(time.Hour)
	messageWarmTimer := time.NewTimer(time.Hour)

	t.Cleanup(func() {
		workTicker.Stop()
		frameTicker.Stop()
		stopTimer(extensionTimer)
		stopTimer(messageWarmTimer)
	})

	return app.runLoopStep(
		context.Background(),
		workTicker,
		frameTicker,
		extensionTimer,
		messageWarmTimer,
		false,
	)
}

func TestDrawDirtyFrame(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		dirty    bool
		wantDraw bool
	}{
		{name: "clean frame skips draw", dirty: false, wantDraw: false},
		{name: "dirty frame draws", dirty: true, wantDraw: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newScrollableRenderTestApp(t)
			screen, ok := app.screen.(*clipboardScreen)
			require.True(t, ok)

			assert.False(t, app.drawDirtyFrame(context.Background(), testCase.dirty))
			assert.Equal(t, testCase.wantDraw, len(screen.content) > 0)
		})
	}
}

func TestDrawLatestResize(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		queuedEvent    tcell.Event
		name           string
		wantComposer   string
		wantLastWidth  int
		wantLastHeight int
	}{
		{
			name:           "uses latest queued resize",
			queuedEvent:    tcell.NewEventResize(100, 40),
			wantComposer:   "",
			wantLastWidth:  100,
			wantLastHeight: 40,
		},
		{
			name:           "stops at pending non-resize event",
			queuedEvent:    tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone),
			wantComposer:   "x",
			wantLastWidth:  80,
			wantLastHeight: 24,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			screen := newClipboardScreen()
			app.screen = screen
			app.renderer = tui.NewRenderer(screen)

			app.screen.EventQ() <- testCase.queuedEvent

			shouldQuit, dirty := app.drawLatestResize(context.Background(), tcell.NewEventResize(80, 24))

			lastWidth, lastHeight := app.lastResize.Size()

			assert.False(t, shouldQuit)
			assert.False(t, dirty)
			assert.Equal(t, testCase.wantComposer, app.composerBuffer.TextValue())
			assert.Equal(t, testCase.wantLastWidth, lastWidth)
			assert.Equal(t, testCase.wantLastHeight, lastHeight)
		})
	}
}

func TestMouseSelectionCopiesFrameText(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	systemClipboard := newMockSystemClipboard()
	expectClipboardWrite(t, systemClipboard, "ello\nwor")
	app := newRenderTestApp(t)
	app.screen = screen
	app.systemClipboard = systemClipboard
	app.frame = tui.NewCellBuffer(8, 2, tcell.StyleDefault)
	tui.WriteCells(app.frame, 0, 0, 8, "hello", tcell.StyleDefault)
	tui.WriteCells(app.frame, 0, 1, 8, "world", tcell.StyleDefault)

	app.handleMouse(tcell.NewEventMouse(1, 0, tcell.ButtonPrimary, tcell.ModNone))
	app.handleMouse(tcell.NewEventMouse(3, 1, tcell.ButtonPrimary, tcell.ModNone))
	app.handleMouse(tcell.NewEventMouse(3, 1, tcell.ButtonNone, tcell.ModNone))

	if got, want := string(screen.clipboard), "ello\nwor"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}

	assertClipboardExpectations(t, systemClipboard)
}

func TestMouseDoubleClickSelectsAndCopiesWord(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	systemClipboard := newMockSystemClipboard()
	expectClipboardWrite(t, systemClipboard, clipboardWorldText)
	app := newRenderTestApp(t)
	app.screen = screen
	app.systemClipboard = systemClipboard
	app.frame = tui.NewCellBuffer(16, 1, tcell.StyleDefault)
	tui.WriteCells(app.frame, 0, 0, 16, "hello world", tcell.StyleDefault)

	firstClick := time.Now()

	app.beginMouseSelection(7, 0, firstClick)
	app.finishMouseSelection(7, 0)
	app.beginMouseSelection(7, 0, firstClick.Add(doubleClickDelay/2))

	if got, want := string(screen.clipboard), clipboardWorldText; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}

	assertClipboardExpectations(t, systemClipboard)

	for column := 6; column < 11; column++ {
		if !app.selection.contains(column, 0) {
			t.Fatalf("column %d on selected word is not selected", column)
		}
	}

	if app.selection.contains(5, 0) || app.selection.contains(11, 0) {
		t.Fatal("word selection includes adjacent whitespace")
	}
}

func TestMouseDoubleClickSelectsWhitespace(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	systemClipboard := newMockSystemClipboard()
	expectClipboardWrite(t, systemClipboard, "   ")
	app := newRenderTestApp(t)
	app.screen = screen
	app.systemClipboard = systemClipboard
	app.frame = tui.NewCellBuffer(16, 1, tcell.StyleDefault)
	tui.WriteCells(app.frame, 0, 0, 16, "hello   world", tcell.StyleDefault)

	firstClick := time.Now()

	app.beginMouseSelection(6, 0, firstClick)
	app.finishMouseSelection(6, 0)
	app.beginMouseSelection(6, 0, firstClick.Add(doubleClickDelay/2))

	if got, want := string(screen.clipboard), "   "; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}

	assertClipboardExpectations(t, systemClipboard)

	for column := 5; column < 8; column++ {
		if !app.selection.contains(column, 0) {
			t.Fatalf("column %d on selected whitespace is not selected", column)
		}
	}
}

func TestMouseFourthClickSelectsAndCopiesLine(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	systemClipboard := newMockSystemClipboard()
	expectClipboardWrite(t, systemClipboard, clipboardWorldText)
	expectClipboardWrite(t, systemClipboard, "hello world")
	app := newRenderTestApp(t)
	app.screen = screen
	app.systemClipboard = systemClipboard
	app.frame = tui.NewCellBuffer(16, 2, tcell.StyleDefault)
	tui.WriteCells(app.frame, 0, 0, 16, "hello", tcell.StyleDefault)
	tui.WriteCells(app.frame, 0, 1, 16, "hello world", tcell.StyleDefault)

	firstClick := time.Now()

	app.beginMouseSelection(7, 1, firstClick)
	app.finishMouseSelection(7, 1)
	app.beginMouseSelection(7, 1, firstClick.Add(doubleClickDelay/4))
	app.beginMouseSelection(7, 1, firstClick.Add(doubleClickDelay/3))
	app.finishMouseSelection(7, 1)
	app.beginMouseSelection(7, 1, firstClick.Add(doubleClickDelay/2))

	if got, want := string(screen.clipboard), "hello world"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}

	assertClipboardExpectations(t, systemClipboard)

	for column := range app.frame.Width() {
		if !app.selection.contains(column, 1) {
			t.Fatalf("column %d on selected line is not selected", column)
		}
	}
}

func TestFlushFrameHighlightsSelection(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.renderer = tui.NewRenderer(screen)
	app.frame = tui.NewCellBuffer(6, 1, tcell.StyleDefault)
	tui.WriteCells(app.frame, 0, 0, 6, "abcdef", app.theme.style(colorText))
	app.selection = mouseSelection{
		lastClickUnixNano: 0,
		startX:            1,
		startY:            0,
		endX:              4,
		endY:              0,
		lastClickX:        0,
		lastClickY:        0,
		clickCount:        0,
		active:            true,
	}

	app.flushFrame()

	if got := app.frame.Cell(0, 0).Style.GetBackground(); got != tcell.ColorDefault {
		t.Fatalf("unselected background = %v, want default", got)
	}

	for column := 1; column < 4; column++ {
		got := app.frame.Cell(column, 0).Style.GetBackground()

		want := app.theme.colors[colorSelectedBg]
		if got != want {
			t.Fatalf("selected background at %d = %v, want %v", column, got, want)
		}
	}
}

func TestApplyPromptResponsePreservesLargerStreamedContextUsage(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.applyTokenUsage(&model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100_000,
		ContextTokens:   14_000,
		InputTokens:     14_000,
		OutputTokens:    0,
	})

	app.applyPromptResponse(context.Background(), &assistant.PromptResponse{
		SessionID:        "test-session",
		UserEntryID:      terminalTestUserID,
		AssistantEntryID: "assistant",
		Text:             "ok",
		Thinking:         nil,
		ToolEvents:       nil,
		Usage: model.TokenUsage{
			Breakdown:       nil,
			TopContributors: nil,
			ContextWindow:   100_000,
			ContextTokens:   12_000,
			InputTokens:     12_000,
			OutputTokens:    700,
		},
		Cached: false,
	}, 0)

	assert.Equal(t, model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100_000,
		ContextTokens:   14_000,
		InputTokens:     0,
		OutputTokens:    0,
	}, app.tokenUsage)
}

func TestHighVolumeStreamEventsDoNotForceImmediateDraw(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	for _, kind := range []asyncEventKind{
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptUsage,
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

func TestScrolledMessageLinesCanReachFullHistory(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	for index := range 20 {
		app.addMessage(transcript.RoleAssistant, "history message "+tui.Int(index))
	}

	bottom := app.messageLines(80, 6)
	if lineIndexContaining(bottom, "history message 19") == -1 {
		t.Fatalf("expected latest history at bottom, got %v", lineTexts(bottom))
	}

	warmMessageLineCache(app)

	app.scrollTranscript(100)

	older := app.messageLines(80, 6)
	if lineIndexContaining(older, "history message 0") == -1 {
		t.Fatalf("expected older history after scrolling, got %v", lineTexts(older))
	}
}

func TestWarmMessageLineCacheStepPrebuildsFullHistoryAfterInitialRender(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	for index := range 20 {
		app.addMessage(transcript.RoleAssistant, "history message "+tui.Int(index))
	}

	_ = app.messageLines(80, 6)
	if app.transcript.LineCache.warm {
		t.Fatal("tail render should not eagerly warm full history")
	}

	warmMessageLineCache(app)

	if !app.transcript.LineCache.warm {
		t.Fatal("expected history cache to be warm")
	}

	if len(app.transcript.LineCache.prefixes) != len(app.transcript.History)+1 {
		t.Fatalf(
			"row prefix length = %d, want %d",
			len(app.transcript.LineCache.prefixes),
			len(app.transcript.History)+1,
		)
	}
}

func TestWarmMessageLineCacheStepStopsWhenNoProgressIsPossible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup func(*App)
		name  string
	}{
		{
			name: "no measured viewport",
			setup: func(*App) {
				// Leave lastMessageMaxRows at zero.
			},
		},
		{
			name: "tools expanded",
			setup: func(a *App) {
				a.transcript.LastMaxRows = 6
				a.toolsExpanded = true
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.addMessage(transcript.RoleAssistant, "history message")
			testCase.setup(app)

			if app.transcript.LineCache.warmStep(app) {
				t.Fatal("cache warm step should not report progress when blocked")
			}

			if app.transcript.LineCache.warm {
				t.Fatal("cache should not warm when progress is blocked")
			}
		})
	}
}

func TestWarmMessageLineCacheStepPrebuildsIncrementally(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	for index := range messageCacheWarmBatchSize + 1 {
		app.addMessage(transcript.RoleAssistant, "history message "+tui.Int(index))
	}

	_ = app.messageLines(80, 6)
	app.transcript.LineCache.warmStep(app)

	if app.transcript.LineCache.warm {
		t.Fatal("single warm step should not eagerly warm full history")
	}

	if app.transcript.LineCache.warmIndex != messageCacheWarmBatchSize {
		t.Fatalf("warm index = %d, want %d", app.transcript.LineCache.warmIndex, messageCacheWarmBatchSize)
	}

	app.transcript.LineCache.warmStep(app)

	if !app.transcript.LineCache.warm {
		t.Fatal("expected second warm step to finish cache")
	}
}

func TestScrolledMessageLinesBeforeWarmCacheUsesVisibleTail(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	for index := range 100 {
		app.addMessage(transcript.RoleAssistant, "history message "+tui.Int(index))
	}

	_ = app.messageLines(80, 6)
	if app.transcript.LineCache.warm {
		t.Fatal("tail render should not warm full history")
	}

	app.scrollTranscript(3)

	lines := app.messageLines(80, 6)
	if app.transcript.LineCache.warm {
		t.Fatal("scroll before cache warm should not synchronously warm full history")
	}

	if lineIndexContaining(lines, "history message 98") == -1 {
		t.Fatalf("expected recent history while warm cache is pending, got %v", lineTexts(lines))
	}
}

func TestScrolledMessageLinesRevalidatesWarmCacheAfterResize(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	for index := range 20 {
		app.addMessage(transcript.RoleAssistant, "history message "+tui.Int(index)+" with enough text to wrap")
	}

	_ = app.messageLines(80, 6)
	warmMessageLineCache(app)

	require.True(t, app.transcript.LineCache.warm)
	require.Equal(t, 80, app.transcript.LineCache.state.Width)

	app.scrollTranscript(3)
	_ = app.messageLines(24, 6)

	require.Equal(t, 24, app.transcript.LineCache.state.Width)
	require.False(t, app.transcript.LineCache.warm)
}

func TestBottomMessageLinesExpandedToolUsesTailWithoutFullRender(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.toolsExpanded = true
	output := strings.Join([]string{
		"line 0",
		"line 1",
		"line 2",
		"line 3",
		"line 4",
		"line 5",
	}, "\n")
	app.addMessage(transcript.RoleToolResult, formatToolEventForUI(&assistant.ToolEvent{
		Name:          "read",
		ArgumentsJSON: "{}",
		DetailsJSON:   "",
		Result:        output,
		Error:         "",
		IsError:       false,
	}))

	lines := app.messageLines(80, 4)

	texts := lineTexts(lines)
	if lineIndexContaining(lines, "line 5") == -1 {
		t.Fatalf("expected latest tool output tail, got %v", texts)
	}

	if lineIndexContaining(lines, "line 0") != -1 {
		t.Fatalf("expected old tool output to stay unrendered in bottom tail, got %v", texts)
	}

	if len(app.transcript.LineCache.items) > 0 && app.transcript.LineCache.items[0].Valid {
		t.Fatal("bottom tail render should not populate full expanded tool cache")
	}
}

func TestMessageLineCacheInvalidatesForThinkingVisibility(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.addMessage(transcript.RoleThinking, "cached thought")

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

func TestLoadInitialMessagesUsesTranscriptHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "load", "")
	require.NoError(t, err)
	_, err = app.runtime.SessionRepository().AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Time{},
		Role:      database.RoleAssistant,
		Content:   "loaded assistant",
		Provider:  "",
		Model:     "",
	})
	require.NoError(t, err)

	app.sessionID = session.ID
	app.resetMessages()

	require.NoError(t, app.loadInitialMessages(ctx))
	require.Len(t, app.transcript.History, 1)
	assert.Equal(t, "loaded assistant", app.transcript.History[0].Content)
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

	got := streamingBlockRoles(app.transcript.Streaming.Blocks)

	want := []transcript.Role{
		transcript.RoleThinking,
		transcript.RoleToolResult,
		transcript.RoleThinking,
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

func TestToolBlockPreservesFileContentIndentation(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	event := newTestToolEvent("read", "func main() {\n\tif ok {\n        fmt.Println(\"yes\")\n\t}\n}")
	lines := app.renderToolMessage(80, newChatMessage(transcript.RoleToolResult, formatToolEventForUI(event)))
	texts := lineTexts(lines)

	if lineIndexContaining(lines, "  \tif ok {") == -1 {
		t.Fatalf("expected tab-indented file line to be preserved, got %#v", texts)
	}

	if lineIndexContaining(lines, "          fmt.Println") == -1 {
		t.Fatalf("expected space-indented file line to be preserved, got %#v", texts)
	}

	app.frame = tui.NewCellBuffer(80, len(lines), tcell.StyleDefault)
	for row, line := range lines {
		app.writeStyledLine(row, 80, line)
	}

	if !strings.Contains(frameText(app.frame), "      if ok {") {
		t.Fatalf("expected rendered tab indentation to occupy cells, frame = %q", frameText(app.frame))
	}
}

func TestToolBlockWrapPreservesContinuationIndentation(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.toolsExpanded = true
	event := newTestToolEvent("read", "    "+strings.Repeat("word ", 20))
	lines := app.renderToolMessage(24, newChatMessage(transcript.RoleToolResult, formatToolEventForUI(event)))
	texts := lineTexts(lines)

	if lineIndexContaining(lines, "      word") == -1 {
		t.Fatalf("expected first wrapped file line to preserve indentation, got %#v", texts)
	}

	if lineIndexContaining(lines, "  word") == -1 {
		t.Fatalf("expected continuation line to keep block indent, got %#v", texts)
	}
}

func TestToolDiffPreservesIndentation(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.toolsExpanded = true
	event := newTestToolEvent("edit", "ok")
	event.DetailsJSON = `{"diff":"+1     indented\n+2 \t tabbed"}`
	lines := app.renderToolMessage(80, newChatMessage(transcript.RoleToolResult, formatToolEventForUI(event)))
	texts := lineTexts(lines)

	if lineIndexContaining(lines, "+1     indented") == -1 {
		t.Fatalf("expected space indentation in diff, got %#v", texts)
	}

	if lineIndexContaining(lines, "+2 \t tabbed") == -1 {
		t.Fatalf("expected tab indentation in diff, got %#v", texts)
	}
}

func TestExtensionRendererSkipsDefaultComposerDraw(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerBuffer.SetText("host text")
	layout := app.defaultRuntimeLayout(40, 12)
	app.frame = tui.NewCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	window := layout.Composer
	window.Renderer = "extension"
	app.extensionUI.Windows[window.Name] = window

	app.drawComposerWindow(&layout)

	if got := frameText(app.frame); strings.Contains(got, "host text") {
		t.Fatalf("extension-rendered composer should skip host draw, frame = %q", got)
	}
}

func TestUIRenderDrawsSpansWithoutClearingLaterSpans(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	layout := app.defaultRuntimeLayout(40, 12)
	app.frame = tui.NewCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	app.extensionUI.Overrides[extui.BufferComposer] = extui.WindowOverride{
		DrawOps: []extension.UIDrawOp{
			{
				Style: extension.UIStyle{FG: "", BG: "", Bold: false, Italic: false},
				Spans: []extension.UISpan{
					{Text: "hot", Style: extension.UIStyle{FG: "accent", BG: "", Bold: true, Italic: false}},
					{Text: " cold", Style: extension.UIStyle{FG: string(colorDim), BG: "", Bold: false, Italic: false}},
				},
				Window: extui.BufferComposer,
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

	first := app.frame.Cell(layout.Composer.X, layout.Composer.Y+1)

	second := app.frame.Cell(layout.Composer.X+4, layout.Composer.Y+1)
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
	app.frame = tui.NewCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	app.extensionUI.Overrides[extui.BufferComposer] = extui.WindowOverride{
		DrawOps: []extension.UIDrawOp{
			{
				Style:  extension.UIStyle{FG: "text", BG: "", Bold: false, Italic: false},
				Spans:  []extension.UISpan{},
				Window: extui.BufferComposer,
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
	if got, want := app.frame.Cell(layout.Composer.X, row).Rune, '語'; got != want {
		t.Fatalf("first cell = %q, want %q", got, want)
	}

	if got, want := app.frame.Cell(layout.Composer.X+1, row).Rune, ' '; got != want {
		t.Fatalf("wide continuation cell = %q, want space", got)
	}

	if got, want := app.frame.Cell(layout.Composer.X+2, row).Rune, 'x'; got != want {
		t.Fatalf("third cell = %q, want %q", got, want)
	}
}

func TestUIRenderClearRegionClipsToWindow(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	layout := app.defaultRuntimeLayout(40, 12)

	app.frame = tui.NewCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	for row := 0; row < layout.Composer.Height; row++ {
		tui.WriteCells(
			app.frame,
			layout.Composer.X,
			layout.Composer.Y+row,
			layout.Composer.Width,
			strings.Repeat("x", layout.Composer.Width),
			tcell.StyleDefault.Foreground(cellcolor.Red),
		)
	}

	app.extensionUI.Overrides[extui.BufferComposer] = extui.WindowOverride{
		DrawOps: []extension.UIDrawOp{
			{
				Style:  extension.UIStyle{FG: string(colorDim), BG: "", Bold: false, Italic: false},
				Spans:  []extension.UISpan{},
				Window: extui.BufferComposer,
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

	cleared := app.frame.Cell(layout.Composer.X+2, layout.Composer.Y+1)
	untouched := app.frame.Cell(layout.Composer.X+1, layout.Composer.Y+1)

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
			app.composerBuffer.SetText(tt.prompt)

			layout := app.defaultRuntimeLayout(80, 24)

			composerBottom := layout.Composer.Y + layout.Composer.Height
			if composerBottom != layout.Status.Y {
				t.Fatalf("composer bottom = %d, status y = %d", composerBottom, layout.Status.Y)
			}
		})
	}
}

func TestComposerLayoutReflowsAfterPromptAndTerminalResize(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerBuffer.SetText(strings.Repeat("wide ", 24))

	wide := app.defaultRuntimeLayout(80, 24)

	narrow := app.defaultRuntimeLayout(32, 24)
	if narrow.Composer.Height <= wide.Composer.Height {
		t.Fatalf(
			"narrow composer height = %d, want greater than wide height %d",
			narrow.Composer.Height,
			wide.Composer.Height,
		)
	}

	if narrow.Composer.Y+narrow.Composer.Height != narrow.Status.Y {
		t.Fatalf(
			"narrow composer bottom = %d, status y = %d",
			narrow.Composer.Y+narrow.Composer.Height,
			narrow.Status.Y,
		)
	}

	app.composerBuffer.SetText("")

	empty := app.defaultRuntimeLayout(32, 24)
	if empty.Composer.Height >= narrow.Composer.Height {
		t.Fatalf(
			"empty composer height = %d, want less than populated height %d",
			empty.Composer.Height,
			narrow.Composer.Height,
		)
	}

	if empty.Composer.Y+empty.Composer.Height != empty.Status.Y {
		t.Fatalf(
			"empty composer bottom = %d, status y = %d",
			empty.Composer.Y+empty.Composer.Height,
			empty.Status.Y,
		)
	}
}

func TestDrawComposerDoesNotPersistDefaultWindowGeometry(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerBuffer.SetText(strings.Repeat("wide ", 24))

	wideLayout := app.defaultRuntimeLayout(80, 24)
	app.frame = tui.NewCellBuffer(wideLayout.Width, wideLayout.Height, tcell.StyleDefault)
	app.drawComposerWindow(&wideLayout)

	if _, ok := app.extensionUI.Windows[extui.BufferComposer]; ok {
		t.Fatal("default composer draw should not persist runtime window geometry")
	}

	narrowLayout := app.currentRuntimeLayoutForSize(32, 24)
	if narrowLayout.Composer.Height <= wideLayout.Composer.Height {
		t.Fatalf(
			"narrow composer height = %d, want greater than wide height %d",
			narrowLayout.Composer.Height,
			wideLayout.Composer.Height,
		)
	}
}

func frameText(frame *tui.CellBuffer) string {
	if frame == nil {
		return ""
	}

	var builder strings.Builder

	for row := range frame.Height() {
		for column := range frame.Width() {
			builder.WriteRune(frame.Cell(column, row).Rune)
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

func newScrollableRenderTestApp(t *testing.T) *App {
	t.Helper()

	screen := newClipboardScreen()
	screen.SetSize(40, 8)

	app := newRenderTestApp(t)
	app.screen = screen
	app.renderer = tui.NewRenderer(screen)

	for range 20 {
		app.addMessage(transcript.RoleAssistant, "scrollable content")
	}

	return app
}

func newTestAsyncEvent(kind asyncEventKind, text string) *asyncEvent {
	return &asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          kind,
		Provider:      "",
		Text:          text,
		PromptID:      0,
	}
}

func newTestToolEvent(name, result string) *assistant.ToolEvent {
	return &assistant.ToolEvent{
		Name:          name,
		ArgumentsJSON: "",
		DetailsJSON:   "",
		Result:        result,
		Error:         "",
		IsError:       false,
	}
}

func assertThinkingLineDim(t *testing.T, app *App, lines []tui.Line) {
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

func streamingBlockRoles(blocks []chatMessage) []transcript.Role {
	roles := make([]transcript.Role, 0, len(blocks))
	for _, block := range blocks {
		roles = append(roles, block.Role)
	}

	return roles
}

func rolesEqual(left, right []transcript.Role) bool {
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

func lineIndexContaining(lines []tui.Line, text string) int {
	for index, line := range lines {
		if strings.Contains(line.Text, text) {
			return index
		}
	}

	return -1
}

func lineTexts(lines []tui.Line) []string {
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}

	return texts
}

func warmMessageLineCache(app *App) {
	for {
		if !app.transcript.LineCache.warmStep(app) {
			return
		}
	}
}
