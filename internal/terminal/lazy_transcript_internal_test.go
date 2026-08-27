package terminal

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/transcript"
	"github.com/omarluq/librecode/internal/tui"
)

func TestAtLoadedTranscriptStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		maxRows     int
		scroll      int
		hasOlder    bool
		wantAtStart bool
	}{
		{name: "no older page", hasOlder: false, maxRows: 2, scroll: 100, wantAtStart: false},
		{name: "viewport not measured", hasOlder: true, maxRows: 0, scroll: 100, wantAtStart: false},
		{name: "below loaded start", hasOlder: true, maxRows: 2, scroll: 9, wantAtStart: false},
		{name: "at loaded start", hasOlder: true, maxRows: 2, scroll: 10, wantAtStart: true},
		{name: "past loaded start", hasOlder: true, maxRows: 2, scroll: 20, wantAtStart: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			for index := range 4 {
				app.addMessage(transcript.RoleAssistant, fmt.Sprintf("message %d", index))
			}

			app.transcript.HasOlder = test.hasOlder
			app.transcript.LastMaxRows = test.maxRows
			app.scrollOffset = test.scroll

			assert.Equal(t, test.wantAtStart, app.atLoadedTranscriptStart())
		})
	}
}

func TestHydrateOlderTranscriptNoOpConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup func(*App)
		name  string
	}{
		{name: "no older page", setup: func(app *App) { app.transcript.HasOlder = false }},
		{name: "no runtime", setup: func(app *App) { app.runtime = nil }},
		{name: "no session", setup: func(app *App) { app.sessionID = "" }},
		{name: "empty history", setup: func(app *App) { app.transcript.History = nil }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
			app.transcript.HasOlder = true
			app.sessionID = testSlashSession
			app.transcript.History = []chatMessage{newChatMessage(transcript.RoleAssistant, "loaded")}
			test.setup(app)
			before := append([]chatMessage(nil), app.transcript.History...)

			require.NoError(t, app.hydrateOlderTranscript(context.Background()))
			assert.Equal(t, before, app.transcript.History)
		})
	}
}

func TestHydrateOlderTranscriptStopsWithoutDurableCursor(t *testing.T) {
	t.Parallel()

	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	app.transcript.HasOlder = true
	app.sessionID = testSlashSession
	app.transcript.History = []chatMessage{newChatMessage(transcript.RoleAssistant, "ephemeral")}

	require.NoError(t, app.hydrateOlderTranscript(context.Background()))
	assert.False(t, app.transcript.HasOlder)
	assert.Equal(t, "ephemeral", app.transcript.History[0].Content)
}

func lazySessionMessage(
	role database.Role,
	content string,
	parts []database.MessagePartEntity,
) database.SessionMessageEntity {
	return database.SessionMessageEntity{
		CreatedAt: time.Time{}, ID: "", SessionID: "", EntryID: "", Sender: "",
		Role: role, Content: content, Provider: "", Model: "", Parts: parts,
	}
}

func TestPrependPromptHistory(t *testing.T) {
	t.Parallel()

	imagePart := database.MessagePartEntity{
		Text: "", Data: []byte{1}, MIMEType: "image/png", Name: "old.png",
		Type: database.MessagePartImage, Width: 2, Height: 3,
	}
	messages := []database.SessionMessageEntity{
		lazySessionMessage(database.RoleUser, "older", []database.MessagePartEntity{imagePart}),
		lazySessionMessage(database.RoleAssistant, "ignored", nil),
		lazySessionMessage(database.RoleUser, "newer", nil),
	}

	app := newRenderTestApp(t)
	app.promptHistory = []string{"current"}
	app.promptHistoryImages = [][]imageAttachment{{}}
	app.promptHistoryIndex = 1
	app.promptHistoryDraft = "draft"
	app.prependPromptHistory(messages)

	assert.Equal(t, []string{"older", "newer", "current"}, app.promptHistory)
	require.Len(t, app.promptHistoryImages, 3)
	require.Len(t, app.promptHistoryImages[0], 1)
	assert.Equal(t, "old.png", app.promptHistoryImages[0][0].Name)
	assert.Equal(t, len(app.promptHistory), app.promptHistoryIndex)
	assert.Empty(t, app.promptHistoryDraft)
}

func TestPrependPromptHistoryKeepsNewestLimit(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.promptHistory = make([]string, promptHistoryLimit)

	app.promptHistoryImages = make([][]imageAttachment, promptHistoryLimit)
	for index := range promptHistoryLimit {
		app.promptHistory[index] = fmt.Sprintf("existing %d", index)
	}

	app.prependPromptHistory([]database.SessionMessageEntity{
		lazySessionMessage(database.RoleUser, "too old", nil),
	})

	require.Len(t, app.promptHistory, promptHistoryLimit)
	assert.Equal(t, "existing 0", app.promptHistory[0])
	assert.Equal(t, fmt.Sprintf("existing %d", promptHistoryLimit-1), app.promptHistory[promptHistoryLimit-1])
}

func TestLoadInitialMessagesReplacesWelcomeWithTail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "tail", "")
	require.NoError(t, err)

	for index := range defaultTerminalHeight + 1 {
		_, err = app.runtime.SessionRepository().AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
			Timestamp: time.Date(2025, 1, 1, 0, 0, index, 0, time.UTC),
			Role:      database.RoleAssistant, Content: fmt.Sprintf("message %02d", index),
			Provider: "", Model: "", Parts: nil,
		})
		require.NoError(t, err)
	}

	app.addWelcomeMessage()
	app.sessionID = session.ID
	require.NoError(t, app.loadInitialMessages(ctx))

	require.Len(t, app.transcript.History, defaultTerminalHeight)
	assert.Equal(t, "message 01", app.transcript.History[0].Content)
	assert.True(t, app.transcript.HasOlder)
	assert.NotContains(t, app.transcript.History[0].Content, welcomeMessagePrefix)
}

func TestSessionMessageTailWithoutSessionStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup func(*App)
		name  string
	}{
		{name: "empty session ID", setup: func(app *App) { app.sessionID = "" }},
		{name: "nil runtime", setup: func(app *App) { app.runtime = nil }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.sessionID = testSlashSession
			test.setup(app)

			messages, hasOlder, err := app.sessionMessageTail(context.Background(), app.sessionID)
			require.NoError(t, err)
			assert.Empty(t, messages)
			assert.False(t, hasOlder)
		})
	}
}

func TestProfileStartupStageRunsOperationWithoutProfiler(t *testing.T) {
	t.Parallel()

	called := false

	profileStartupStage(nil, "transcript", func() { called = true })
	assert.True(t, called)
}

func TestNeedsRuntimeRenderPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup func(*App)
		name  string
		want  bool
	}{
		{name: "default UI", setup: func(*App) {}, want: false},
		{name: "custom layout", setup: func(app *App) {
			app.extensionUI.Layout = &extension.LayoutState{Windows: nil, Width: 0, Height: 0}
		}, want: true},
		{name: "custom window", setup: func(app *App) {
			app.extensionUI.Windows["custom"] = app.defaultRuntimeLayout(40, 12).Transcript
		}, want: true},
		{name: "UI override", setup: func(app *App) {
			app.extensionUI.Overrides[extui.BufferComposer] = extui.WindowOverride{DrawOps: nil, Reset: false}
		}, want: true},
		{name: "cursor override", setup: func(app *App) {
			app.extensionUI.Cursor = &extension.UICursor{Window: "", Row: 0, Col: 0}
		}, want: true},
		{name: "transcript buffer", setup: func(app *App) {
			app.extensionUI.Buffers[extui.BufferTranscript] = extension.BufferState{
				Metadata: nil, Blocks: nil, Name: "", Text: "", Label: "", Chars: nil, Cursor: 0,
			}
		}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			test.setup(app)
			assert.Equal(t, test.want, app.needsRuntimeRenderPath())
		})
	}
}

func TestDrawTinyTerminal(t *testing.T) {
	t.Parallel()

	app := newScrollableRenderTestApp(t)
	screen, ok := app.screen.(*clipboardScreen)
	require.True(t, ok)

	screen.size = [2]int{12, 4}

	app.draw(context.Background())

	assert.Contains(t, frameText(app.frame), "librecode:")
	assert.Equal(t, 12, app.frame.Width())
	assert.Equal(t, 4, app.frame.Height())
}

func TestDrawTinyTruncatesToWidth(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.frame = tui.NewCellBuffer(5, 3, tcell.StyleDefault)
	app.drawTiny(5, 3)

	assert.Contains(t, frameText(app.frame), "libr")
}
