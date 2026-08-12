package terminal

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/transcript"
)

type terminalSteeringCancelCompleter struct {
	firstStarted  chan *assistant.CompletionRequest
	checkpoint    chan []llm.Message
	proceed       chan struct{}
	second        chan *assistant.CompletionRequest
	releaseSecond chan struct{}
	lock          sync.Mutex
	calls         int
}

func newTerminalSteeringCancelCompleter() *terminalSteeringCancelCompleter {
	return &terminalSteeringCancelCompleter{
		firstStarted:  make(chan *assistant.CompletionRequest, 1),
		checkpoint:    make(chan []llm.Message, 1),
		proceed:       make(chan struct{}),
		second:        make(chan *assistant.CompletionRequest, 1),
		releaseSecond: make(chan struct{}),
		lock:          sync.Mutex{},
		calls:         0,
	}
}

func (client *terminalSteeringCancelCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.lock.Lock()
	client.calls++
	call := client.calls
	client.lock.Unlock()

	if call > 1 {
		client.second <- request

		select {
		case <-client.releaseSecond:
			return newTerminalCompletionResult("follow-up done"), nil
		case <-ctx.Done():
			return nil, fmt.Errorf("follow-up completion canceled: %w", ctx.Err())
		}
	}

	client.firstStarted <- request

	select {
	case <-client.proceed:
	case <-ctx.Done():
		return nil, fmt.Errorf("completion canceled before checkpoint: %w", ctx.Err())
	}

	messages, err := request.OnRoundCheckpoint(ctx, &llm.CompletedRound{
		Assistant: llm.Message{
			Metadata: nil, Role: llm.RoleAssistant, Content: []llm.Part{llm.TextPart("settled round")},
		},
		ToolResults: nil, FinishReason: llm.FinishReasonStop, Usage: llm.EmptyUsage(),
	})
	if err != nil {
		return nil, fmt.Errorf("checkpoint: %w", err)
	}

	client.checkpoint <- messages

	<-ctx.Done()

	return nil, fmt.Errorf("completion canceled after checkpoint: %w", ctx.Err())
}

func TestTerminalRuntimeCancelRestoresOnlyUnconsumedSteeringBeforeFollowUp(t *testing.T) {
	t.Parallel()

	client := newTerminalSteeringCancelCompleter()
	app := newPromptSendTestApp(t, client)
	app.screen = newClipboardScreen()
	app.sendPrompt(t.Context(), "initial")

	<-client.firstStarted

	entryEvent := readPromptAsyncEvent(t, app)
	require.Equal(t, asyncEventPromptUserEntry, entryEvent.Kind)
	app.handlePromptAsyncEvent(t.Context(), entryEvent)

	app.composerBuffer.SetText("consumed steering")
	_, err := app.handleInputKey(t.Context(), tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	require.NoError(t, err)
	close(client.proceed)

	checkpoint := <-client.checkpoint
	require.Len(t, checkpoint, 1)
	assert.Equal(t, "consumed steering", checkpoint[0].Content[0].Text)

	app.composerBuffer.SetText("unconsumed steering")
	_, err = app.handleInputKey(t.Context(), tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	require.NoError(t, err)
	app.composerBuffer.SetText(promptSendQueuedFollowUp)
	_, err = app.handleInputKey(t.Context(), tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModShift))
	require.NoError(t, err)
	assert.Equal(t, []string{promptSendQueuedFollowUp}, promptDraftTexts(app.queuedMessages))

	app.cancelActivePrompt(t.Context())

	returnedEvent := readPromptAsyncEventUntilKind(t, app, asyncEventSteeringReturn)

	app.handlePromptAsyncEvent(t.Context(), returnedEvent)
	assert.Equal(t, []string{"unconsumed steering", promptSendQueuedFollowUp}, promptDraftTexts(app.queuedMessages))

	errorEvent := readPromptAsyncEventUntilKind(t, app, asyncEventPromptError)

	persisted, err := app.runtime.SessionRepository().Messages(t.Context(), app.sessionID)
	require.NoError(t, err)
	require.Len(t, persisted, 4)
	assert.Equal(t, []database.Role{
		database.RoleUser, database.RoleAssistant, database.RoleUser, database.RoleCustom,
	}, []database.Role{persisted[0].Role, persisted[1].Role, persisted[2].Role, persisted[3].Role})
	assert.Equal(t, []string{"initial", "settled round", "consumed steering"}, []string{
		persisted[0].Content, persisted[1].Content, persisted[2].Content,
	})
	assert.Equal(t, "[system] response canceled by user", persisted[3].Content)

	app.handlePromptAsyncEvent(t.Context(), errorEvent)

	secondRequest := <-client.second
	require.NotEmpty(t, secondRequest.Messages)
	assert.Equal(t, "unconsumed steering", secondRequest.Messages[len(secondRequest.Messages)-1].Content)
	assert.Equal(t, []string{promptSendQueuedFollowUp}, promptDraftTexts(app.queuedMessages))
	require.NotNil(t, app.activePrompt)
	assert.Equal(t, "unconsumed steering", app.activePrompt.Prompt)
	close(client.releaseSecond)
}

func readPromptAsyncEventUntilKind(t *testing.T, app *App, want asyncEventKind) *asyncEvent {
	t.Helper()

	for {
		event := readPromptAsyncEvent(t, app)
		if event.Kind == want {
			return event
		}

		app.handlePromptAsyncEvent(t.Context(), event)
	}
}

func TestCancelActivePromptPreservesQueuedMessages(t *testing.T) {
	t.Parallel()

	canceled := false
	app := newRenderTestApp(t)
	app.working = true
	app.addMessage(transcript.RoleUser, "prompt")
	app.appendStreamingBlock(transcript.RoleAssistant, "partial")

	image := imageAttachment{
		Name: testQueuedImageName, MIMEType: clipboardImageMIME, Data: []byte{1, 2}, Width: 2, Height: 3,
	}
	app.queuedMessages = []promptDraft{{Text: "follow up", Images: []imageAttachment{image}}}
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
	assert.Equal(t, []string{"follow up"}, promptDraftTexts(app.queuedMessages))
	require.Len(t, app.queuedMessages[0].Images, 1)
	assert.Equal(t, image, app.queuedMessages[0].Images[0])
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

	waitCtx, cancelWait := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelWait()

	request := client.waitForRequest(waitCtx, t)
	client.waitForPromptEntry(waitCtx, t)
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
	request     *assistant.CompletionRequest
	ready       chan struct{}
	promptReady chan struct{}
	once        sync.Once
}

func newCancelPreserveCompleter() *cancelPreserveCompleter {
	return &cancelPreserveCompleter{
		request:     nil,
		ready:       make(chan struct{}),
		promptReady: make(chan struct{}),
		once:        sync.Once{},
	}
}

func (completer *cancelPreserveCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	completer.request = request
	completer.once.Do(func() {
		close(completer.ready)

		if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Content == "keep progress" {
			close(completer.promptReady)
		}
	})
	<-ctx.Done()

	return nil, fmt.Errorf("completion canceled: %w", ctx.Err())
}

func (completer *cancelPreserveCompleter) waitForRequest(
	ctx context.Context,
	t *testing.T,
) *assistant.CompletionRequest {
	t.Helper()

	select {
	case <-completer.ready:
		require.NotNil(t, completer.request)

		return completer.request
	case <-ctx.Done():
		require.FailNow(t, "timed out waiting for completion request", ctx.Err().Error())

		return nil
	}
}

func (completer *cancelPreserveCompleter) waitForPromptEntry(ctx context.Context, t *testing.T) {
	t.Helper()

	select {
	case <-completer.promptReady:
		return
	case <-ctx.Done():
		require.FailNow(t, "timed out waiting for prompt entry in completion request", ctx.Err().Error())
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
