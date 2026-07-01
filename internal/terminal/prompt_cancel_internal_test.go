package terminal

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/transcript"
)

func TestCancelActivePromptPreservesQueuedMessages(t *testing.T) {
	t.Parallel()

	canceled := false
	app := newRenderTestApp(t)
	app.working = true
	app.addMessage(transcript.RoleUser, "prompt")
	app.appendStreamingBlock(transcript.RoleAssistant, "partial")
	app.queuedMessages = []string{"follow up"}
	app.activePrompt = newTestActivePrompt(func() { canceled = true })
	app.activePrompt.Prompt = "prompt"

	app.cancelActivePrompt(context.Background())

	assert.True(t, canceled)
	assert.True(t, app.activePrompt.Canceled)
	assert.True(t, app.working)
	require.Len(t, app.transcript.History, 1)
	assert.Equal(t, "prompt", app.transcript.History[0].Content)
	require.Len(t, app.transcript.Streaming.Blocks, 1)
	assert.Equal(t, "partial", app.transcript.Streaming.Blocks[0].Content)
	assert.Equal(t, []string{"follow up"}, app.queuedMessages)
	assert.Equal(t, "canceling response...", app.statusMessage)
}

func TestCancelActivePromptIsIdempotentWhileCanceling(t *testing.T) {
	t.Parallel()

	cancelCalls := 0
	app := newRenderTestApp(t)
	app.working = true
	app.activePrompt = newTestActivePrompt(func() { cancelCalls++ })

	app.cancelActivePrompt(context.Background())
	app.cancelActivePrompt(context.Background())

	assert.Equal(t, 1, cancelCalls)
	require.NotNil(t, app.activePrompt)
	assert.True(t, app.activePrompt.Canceled)
	assert.Equal(t, "canceling response...", app.statusMessage)
}

func TestCancelActivePromptPreservesPersistedProgress(t *testing.T) {
	t.Parallel()

	client := newCancelPreserveCompleter()
	app := newPromptSendTestApp(t, client)
	app.screen = newClipboardScreen()
	app.sendPrompt(context.Background(), "keep progress")

	userEntryEvent := readPromptAsyncEvent(t, app)
	require.Equal(t, asyncEventPromptUserEntry, userEntryEvent.Kind)
	app.handlePromptAsyncEvent(context.Background(), userEntryEvent)

	request := client.waitForRequest(t)
	client.waitForPromptEntry(t)
	request.OnEvent(assistant.StreamEvent{
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          assistant.StreamEventTextDelta,
		Text:          "partial",
	})
	handlePromptAsyncEventUntil(t, app, asyncEventPromptDelta)
	app.cancelActivePrompt(context.Background())
	handlePromptAsyncEventUntil(t, app, asyncEventPromptError)

	messages, err := app.runtime.SessionRepository().Messages(context.Background(), userEntryEvent.Provider)
	require.NoError(t, err)
	require.Len(t, messages, 3)
	assert.Equal(t, database.RoleUser, messages[0].Role)
	assert.Equal(t, database.RoleAssistant, messages[1].Role)
	assert.Equal(t, "partial", messages[1].Content)
	assert.Equal(t, database.RoleCustom, messages[2].Role)
	assert.Equal(t, "[system] response canceled by user", messages[2].Content)
}

type cancelPreserveCompleter struct {
	request *assistant.CompletionRequest
	ready   chan struct{}
	once    sync.Once
}

func newCancelPreserveCompleter() *cancelPreserveCompleter {
	return &cancelPreserveCompleter{
		request: nil,
		ready:   make(chan struct{}),
		once:    sync.Once{},
	}
}

func (completer *cancelPreserveCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	completer.request = request
	completer.once.Do(func() { close(completer.ready) })
	<-ctx.Done()

	return nil, fmt.Errorf("completion canceled: %w", ctx.Err())
}

func (completer *cancelPreserveCompleter) waitForRequest(t *testing.T) *assistant.CompletionRequest {
	t.Helper()

	select {
	case <-completer.ready:
		return completer.request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion request")

		return nil
	}
}

func (completer *cancelPreserveCompleter) waitForPromptEntry(t *testing.T) {
	t.Helper()

	for {
		request := completer.request
		if request != nil && request.Messages[len(request.Messages)-1].Content == "keep progress" {
			return
		}

		select {
		case <-time.After(10 * time.Millisecond):
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for prompt entry in completion request")
		}
	}
}

func handlePromptAsyncEventUntil(t *testing.T, app *App, wantKind asyncEventKind) {
	t.Helper()

	for range 10 {
		event := readPromptAsyncEvent(t, app)
		app.handlePromptAsyncEvent(context.Background(), event)

		if event.Kind == wantKind {
			return
		}
	}

	t.Fatalf("timed out waiting for async event kind %q", wantKind)
}

func TestCancelActivePromptWithoutActivePromptClearsTransientState(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.working = true
	app.streamingText = "partial"
	app.streamingThinkingText = "thinking"
	app.transcript.Streaming.Blocks = []chatMessage{newChatMessage(transcript.RoleAssistant, "partial")}
	app.streamedToolEvents = 2

	app.cancelActivePrompt(context.Background())

	assert.False(t, app.working)
	assert.Empty(t, app.streamingText)
	assert.Empty(t, app.streamingThinkingText)
	assert.Empty(t, app.transcript.Streaming.Blocks)
	assert.Zero(t, app.streamedToolEvents)
	assert.Equal(t, "no active response to cancel", app.statusMessage)
}
