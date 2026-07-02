package terminal

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/transcript"
)

const (
	asyncTestQueuedText = "queued"
	asyncTestSessionID  = "async-session"
	asyncTestEntryID    = "async-entry"
	asyncTestToolName   = "async-read"
	asyncTestGreeting   = "async-hello"
	asyncTestThinking   = "async-thinking"
	asyncTestToolStart  = "async-bash"
	asyncTestCompact    = "async context auto-compacted"
	asyncTestIgnored    = "async-ignored"
)

type promptHandlerCase struct {
	invoke   func(context.Context, *App)
	assert   func(*testing.T, *asyncEvent)
	name     string
	wantKind asyncEventKind
}

func TestAsyncPromptHandlersPostEvents(t *testing.T) {
	t.Parallel()

	for _, testCase := range asyncPromptHandlerCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.screen = newClipboardScreen()

			testCase.invoke(context.Background(), app)
			event := readPromptAsyncEvent(t, app)

			require.Equal(t, testCase.wantKind, event.Kind)
			testCase.assert(t, event)
		})
	}
}

func asyncPromptHandlerCases() []promptHandlerCase {
	return []promptHandlerCase{
		{
			name: "user entry",
			invoke: func(ctx context.Context, app *App) {
				app.promptUserEntryHandler(ctx, 7)(assistant.PromptUserEntryEvent{
					SessionID: asyncTestSessionID,
					EntryID:   asyncTestEntryID,
				})
			},
			wantKind: asyncEventPromptUserEntry,
			assert: func(t *testing.T, event *asyncEvent) {
				t.Helper()
				assert.Equal(t, uint64(7), event.PromptID)
				assert.Equal(t, asyncTestSessionID, event.Provider)
				assert.Equal(t, asyncTestEntryID, event.Text)
			},
		},
		{
			name: "retry start",
			invoke: func(ctx context.Context, app *App) {
				app.promptRetryHandler(ctx, 8)(assistant.RetryEvent{
					Kind:        assistant.RetryEventStart,
					Error:       "",
					Attempt:     1,
					MaxAttempts: 2,
					Delay:       1500 * time.Millisecond,
				})
			},
			wantKind: asyncEventPromptRetry,
			assert: func(t *testing.T, event *asyncEvent) {
				t.Helper()
				assert.Equal(t, uint64(8), event.PromptID)
				assert.Equal(t, string(assistant.RetryEventStart), event.Provider)
				assert.Equal(t, "retrying model request in 2s", event.Text)
			},
		},
		{
			name: "retry end",
			invoke: func(ctx context.Context, app *App) {
				app.promptRetryHandler(ctx, 9)(assistant.RetryEvent{
					Kind:        assistant.RetryEventEnd,
					Error:       "",
					Attempt:     2,
					MaxAttempts: 2,
					Delay:       0,
				})
			},
			wantKind: asyncEventPromptRetry,
			assert: func(t *testing.T, event *asyncEvent) {
				t.Helper()
				assert.Equal(t, string(assistant.RetryEventEnd), event.Provider)
				assert.Equal(t, "retrying model request", event.Text)
			},
		},
	}
}

type streamHandlerPostCase struct {
	streamEvent assistant.StreamEvent
	name        string
	wantText    string
	wantKind    asyncEventKind
	wantTool    bool
	wantUsage   bool
}

func TestPromptStreamHandlerPostsEvents(t *testing.T) {
	t.Parallel()

	for _, testCase := range promptStreamHandlerPostCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.screen = newClipboardScreen()

			app.promptStreamHandler(context.Background(), 11)(testCase.streamEvent)
			event := readPromptAsyncEvent(t, app)

			require.Equal(t, testCase.wantKind, event.Kind)
			assert.Equal(t, uint64(11), event.PromptID)
			assert.Equal(t, testCase.wantText, event.Text)
			assert.Equal(t, testCase.wantTool, event.ToolEvent != nil)
			assert.Equal(t, testCase.wantUsage, event.Usage != nil)
		})
	}
}

func TestAsyncEventFromStreamEventRejectsUnknownKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind assistant.StreamEventKind
		name string
	}{
		{name: "explicit unknown", kind: assistant.StreamEventUnknown},
		{name: "future unknown", kind: assistant.StreamEventKind("future")},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			event, ok := asyncEventFromStreamEvent(asyncTestStreamEvent(testCase.kind, asyncTestIgnored, nil, nil), 1)

			assert.False(t, ok)
			assert.Nil(t, event)
		})
	}
}

func promptStreamHandlerPostCases() []streamHandlerPostCase {
	return append(promptStreamContentEventCases(), promptStreamContextEventCases()...)
}

func promptStreamContentEventCases() []streamHandlerPostCase {
	usage := asyncTestUsage()
	toolEvent := asyncTestToolEvent()

	return []streamHandlerPostCase{
		{
			name:        "text delta",
			streamEvent: asyncTestStreamEvent(assistant.StreamEventTextDelta, asyncTestGreeting, nil, nil),
			wantKind:    asyncEventPromptDelta,
			wantText:    asyncTestGreeting,
			wantTool:    false,
			wantUsage:   false,
		},
		{
			name:        "thinking delta",
			streamEvent: asyncTestStreamEvent(assistant.StreamEventThinkingDelta, asyncTestThinking, nil, nil),
			wantKind:    asyncEventPromptThinkingDelta,
			wantText:    asyncTestThinking,
			wantTool:    false,
			wantUsage:   false,
		},
		{
			name:        "tool start",
			streamEvent: asyncTestStreamEvent(assistant.StreamEventToolStart, asyncTestToolStart, nil, nil),
			wantKind:    asyncEventPromptToolStart,
			wantText:    asyncTestToolStart,
			wantTool:    false,
			wantUsage:   false,
		},
		{
			name:        "tool result",
			streamEvent: asyncTestStreamEvent(assistant.StreamEventToolResult, "", toolEvent, nil),
			wantKind:    asyncEventPromptToolResult,
			wantText:    "",
			wantTool:    true,
			wantUsage:   false,
		},
		{
			name:        "skill loaded",
			streamEvent: asyncTestStreamEvent(assistant.StreamEventSkillLoaded, "", toolEvent, nil),
			wantKind:    asyncEventPromptToolResult,
			wantText:    "",
			wantTool:    true,
			wantUsage:   false,
		},
		{
			name:        "usage",
			streamEvent: asyncTestStreamEvent(assistant.StreamEventUsage, "", nil, usage),
			wantKind:    asyncEventPromptUsage,
			wantText:    "",
			wantTool:    false,
			wantUsage:   true,
		},
		{
			name:        "usage snapshot",
			streamEvent: asyncTestStreamEvent(assistant.StreamEventUsageSnapshot, "", nil, usage),
			wantKind:    asyncEventPromptUsageSnapshot,
			wantText:    "",
			wantTool:    false,
			wantUsage:   true,
		},
	}
}

func promptStreamContextEventCases() []streamHandlerPostCase {
	return []streamHandlerPostCase{
		promptStreamContextEventCase(
			"context compaction info",
			assistant.StreamEventContextCompaction,
			asyncEventPromptContext,
		),
		promptStreamContextEventCase(
			"context compaction start",
			assistant.StreamEventContextCompactionStart,
			asyncEventCompactStart,
		),
		promptStreamContextEventCase(
			"context compaction done",
			assistant.StreamEventContextCompactionDone,
			asyncEventCompactDone,
		),
		promptStreamContextEventCase(
			"context compaction error",
			assistant.StreamEventContextCompactionError,
			asyncEventCompactError,
		),
	}
}

func promptStreamContextEventCase(
	name string,
	streamKind assistant.StreamEventKind,
	wantKind asyncEventKind,
) streamHandlerPostCase {
	return streamHandlerPostCase{
		name:        name,
		streamEvent: asyncTestStreamEvent(streamKind, asyncTestCompact, nil, nil),
		wantKind:    wantKind,
		wantText:    asyncTestCompact,
		wantTool:    false,
		wantUsage:   false,
	}
}

func TestHandleInterruptRoutesAsyncEvents(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 12, QueuedStart: 0}

	handled, err := app.handleInterrupt(context.Background(), tcell.NewEventInterrupt(&asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventCompactDone,
		Provider:      "compact-entry",
		Text:          compactedStatusMessage,
		PromptID:      12,
	}))

	require.NoError(t, err)
	assert.False(t, handled)
	assert.False(t, app.compacting)
	require.NotNil(t, app.pendingParentID)
	assert.Equal(t, "compact-entry", *app.pendingParentID)

	handled, err = app.handleInterrupt(context.Background(), tcell.NewEventInterrupt("not async"))
	require.NoError(t, err)
	assert.False(t, handled)
}

func TestHandleAuthAsyncEventTransitions(t *testing.T) {
	t.Parallel()

	for _, testCase := range authAsyncEventTransitionCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.authWorking = true

			handled := app.handleAuthAsyncEvent(&asyncEvent{
				Response:      nil,
				ToolCallEvent: nil,
				ToolEvent:     nil,
				Usage:         nil,
				Kind:          testCase.kind,
				Provider:      testCase.provider,
				Text:          testCase.text,
				PromptID:      0,
			})

			assert.Equal(t, testCase.wantHandled, handled)
			assert.Equal(t, testCase.wantAuthWorking, app.authWorking)

			if testCase.wantLastMessage != "" {
				require.NotEmpty(t, app.transcript.History)
				last := app.transcript.History[len(app.transcript.History)-1].Content
				assert.Equal(t, testCase.wantLastMessage, last)
			}
		})
	}
}

type authTransitionCase struct {
	kind            asyncEventKind
	name            string
	text            string
	provider        string
	wantLastMessage string
	wantAuthWorking bool
	wantHandled     bool
}

func authAsyncEventTransitionCases() []authTransitionCase {
	return []authTransitionCase{
		{
			kind:            asyncEventAuthURL,
			name:            "auth URL",
			text:            "open browser",
			provider:        "",
			wantLastMessage: "open browser",
			wantAuthWorking: true,
			wantHandled:     true,
		},
		{
			kind:            asyncEventAuthDone,
			name:            "auth done",
			text:            "",
			provider:        "openai",
			wantLastMessage: "logged in to OpenAI",
			wantAuthWorking: false,
			wantHandled:     true,
		},
		{
			kind:            asyncEventAuthError,
			name:            "auth error",
			text:            "auth failed",
			provider:        "",
			wantLastMessage: "auth failed",
			wantAuthWorking: false,
			wantHandled:     true,
		},
		{
			kind:            asyncEventPromptDelta,
			name:            "non auth",
			text:            asyncTestIgnored,
			provider:        "",
			wantLastMessage: "",
			wantAuthWorking: true,
			wantHandled:     false,
		},
	}
}

func TestPromptAsyncEventHelpers(t *testing.T) {
	t.Parallel()

	for _, kind := range []asyncEventKind{
		asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptRetry,
		asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot,
		asyncEventPromptError,
		asyncEventPromptContext,
		asyncEventCompactStart,
		asyncEventCompactDone,
		asyncEventCompactError,
	} {
		assert.True(t, isPromptAsyncEvent(kind), "kind %q", kind)
	}

	for _, kind := range []asyncEventKind{
		asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventKind("unknown"),
	} {
		assert.False(t, isPromptAsyncEvent(kind), "kind %q", kind)
	}
}

type promptLifecycleCase struct {
	payload     *asyncEvent
	setup       func(*App)
	assert      func(*testing.T, *App)
	name        string
	wantHandled bool
}

func TestPromptLifecycleEvents(t *testing.T) {
	t.Parallel()

	for _, testCase := range promptLifecycleEventCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			testCase.setup(app)

			handled := app.handlePromptLifecycleEvent(context.Background(), testCase.payload)

			assert.Equal(t, testCase.wantHandled, handled)
			testCase.assert(t, app)
		})
	}
}

func promptCompactionLifecycleEventCases() []promptLifecycleCase {
	return []promptLifecycleCase{
		{
			name:    "prompt context start",
			payload: asyncTestEvent(asyncEventCompactStart, "", asyncTestCompact, 1),
			setup:   func(*App) {},
			assert: func(t *testing.T, app *App) {
				t.Helper()

				last := app.transcript.History[len(app.transcript.History)-1].Content
				assert.Equal(t, asyncTestCompact, last)
				assert.True(t, app.compacting)
				assert.Equal(t, "compacting context", app.statusMessage)
			},
			wantHandled: true,
		},
		{
			name:    "prompt context done",
			payload: asyncTestEvent(asyncEventCompactDone, "", asyncTestCompact, 1),
			setup: func(app *App) {
				app.compacting = true
			},
			assert: func(t *testing.T, app *App) {
				t.Helper()
				assert.False(t, app.compacting)
				assert.Equal(t, compactedStatusMessage, app.statusMessage)
			},
			wantHandled: true,
		},
		{
			name:    "prompt context done with empty text",
			payload: asyncTestEvent(asyncEventCompactDone, "", "", 1),
			setup: func(app *App) {
				app.compacting = true
				app.statusMessage = "previous status"
			},
			assert: func(t *testing.T, app *App) {
				t.Helper()
				assert.False(t, app.compacting)
				assert.Equal(t, "previous status", app.statusMessage)
			},
			wantHandled: true,
		},
		{
			name:    "prompt context error",
			payload: asyncTestEvent(asyncEventCompactError, "", asyncTestCompact, 1),
			setup: func(app *App) {
				app.compacting = true
			},
			assert: func(t *testing.T, app *App) {
				t.Helper()
				assert.False(t, app.compacting)
				assert.Equal(t, asyncTestCompact, app.transcript.History[len(app.transcript.History)-1].Content)
			},
			wantHandled: true,
		},
		{
			name:    "plain context notice",
			payload: asyncTestEvent(asyncEventPromptContext, "", asyncTestCompact, 1),
			setup:   func(*App) {},
			assert: func(t *testing.T, app *App) {
				t.Helper()
				assert.False(t, app.compacting)
				assert.Equal(t, asyncTestCompact, app.transcript.History[len(app.transcript.History)-1].Content)
			},
			wantHandled: true,
		},
	}
}

func promptLifecycleEventCases() []promptLifecycleCase {
	return append(promptCompactionLifecycleEventCases(), []promptLifecycleCase{
		{
			name:    "prompt retry",
			payload: asyncTestEvent(asyncEventPromptRetry, string(assistant.RetryEventStart), "retrying", 1),
			setup:   func(*App) {},
			assert: func(t *testing.T, app *App) {
				t.Helper()
				assert.Equal(t, "retrying", app.statusMessage)
			},
			wantHandled: true,
		},
		{
			name:    "prompt user entry",
			payload: asyncTestEvent(asyncEventPromptUserEntry, asyncTestSessionID, asyncTestEntryID, 3),
			setup: func(app *App) {
				app.activePrompt = newTestActivePrompt(nil)
				app.activePrompt.ID = 3
			},
			assert: func(t *testing.T, app *App) {
				t.Helper()
				require.NotNil(t, app.activePrompt)
				assert.Equal(t, asyncTestSessionID, app.activePrompt.SessionID)
				assert.Equal(t, asyncTestEntryID, app.activePrompt.UserEntryID)
			},
			wantHandled: true,
		},
		{
			name:    "prompt error",
			payload: asyncTestEvent(asyncEventPromptError, "", "provider failed", 4),
			setup: func(app *App) {
				app.activePrompt = newTestActivePrompt(nil)
				app.activePrompt.ID = 4
				app.working = true
			},
			assert: func(t *testing.T, app *App) {
				t.Helper()
				assert.False(t, app.working)
				last := app.transcript.History[len(app.transcript.History)-1].Content
				assert.Equal(t, "provider failed", last)
			},
			wantHandled: true,
		},
		{
			name:        "prompt stream event not handled as lifecycle",
			payload:     asyncTestEvent(asyncEventPromptDelta, "", "delta", 5),
			setup:       func(*App) {},
			assert:      func(*testing.T, *App) {},
			wantHandled: false,
		},
	}...)
}

func TestHandlePromptAsyncEventIgnoresStalePromptEvents(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = newTestActivePrompt(nil)
	app.activePrompt.ID = 1

	app.handlePromptAsyncEvent(context.Background(), asyncTestEvent(asyncEventPromptDelta, "", "stale", 2))

	assert.Empty(t, app.transcript.Streaming.Blocks)
}

func TestHandlePromptAsyncEventAppliesCompactionWithoutActivePrompt(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true

	app.handlePromptAsyncEvent(
		context.Background(),
		asyncTestEvent(asyncEventCompactDone, "", compactedStatusMessage, 2),
	)

	assert.False(t, app.compacting)
	assert.Equal(t, compactedStatusMessage, app.statusMessage)
}

func TestApplyPromptErrorProcessesQueuedPrompt(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("queued response"), nil)
	app := newPromptSendTestApp(t, client)
	app.activePrompt = newTestActivePrompt(nil)
	app.activePrompt.ID = 4
	app.working = true
	app.queuedMessages = []string{asyncTestQueuedText}

	app.applyPromptError(context.Background(), "provider failed", app.activePrompt.ID)

	waitForPromptRequest(t, client)
	assert.True(t, app.working)
	assert.True(t, slices.Equal(app.queuedMessages, []string(nil)))
}

type streamEventApplyCase struct {
	payload        *asyncEvent
	assert         func(*testing.T, *App)
	prepare        func(*App)
	name           string
	canceledActive bool
}

func TestHandlePromptStreamEventAppliesStreamData(t *testing.T) {
	t.Parallel()

	for _, testCase := range promptStreamEventApplyCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			if testCase.prepare != nil {
				testCase.prepare(app)
			}

			if testCase.canceledActive {
				app.activePrompt = newTestActivePrompt(nil)
				app.activePrompt.Canceled = true
			}

			app.handlePromptStreamEvent(context.Background(), testCase.payload)

			testCase.assert(t, app)
		})
	}
}

func promptStreamEventApplyCases() []streamEventApplyCase {
	usage := asyncTestUsage()
	toolEvent := asyncTestToolEvent()

	return []streamEventApplyCase{
		{
			name:    "text delta appends streaming assistant",
			payload: asyncTestEvent(asyncEventPromptDelta, "", asyncTestGreeting, 1),
			assert: func(t *testing.T, app *App) {
				t.Helper()
				require.Len(t, app.transcript.Streaming.Blocks, 1)
				assert.Equal(t, transcript.RoleAssistant, app.transcript.Streaming.Blocks[0].Role)
				assert.Equal(t, asyncTestGreeting, app.transcript.Streaming.Blocks[0].Content)
			},
			prepare:        nil,
			canceledActive: false,
		},
		{
			name:    "thinking delta appends streaming thinking",
			payload: asyncTestEvent(asyncEventPromptThinkingDelta, "", asyncTestThinking, 1),
			assert: func(t *testing.T, app *App) {
				t.Helper()
				require.Len(t, app.transcript.Streaming.Blocks, 1)
				assert.Equal(t, transcript.RoleThinking, app.transcript.Streaming.Blocks[0].Role)
			},
			prepare:        nil,
			canceledActive: false,
		},
		{
			name:    "tool result appends tool block",
			payload: asyncTestEventWithTool(asyncEventPromptToolResult, toolEvent),
			assert: func(t *testing.T, app *App) {
				t.Helper()
				require.Len(t, app.transcript.Streaming.Blocks, 1)
				assert.Equal(t, transcript.RoleToolResult, app.transcript.Streaming.Blocks[0].Role)
				assert.Equal(t, 1, app.streamedToolEvents)
			},
			prepare:        nil,
			canceledActive: false,
		},
		{
			name:    "usage updates token usage",
			payload: asyncTestEventWithUsage(asyncEventPromptUsage, usage),
			assert: func(t *testing.T, app *App) {
				t.Helper()
				assert.Equal(t, 25, app.tokenUsage.ContextTokens)
			},
			prepare:        nil,
			canceledActive: false,
		},
		{
			name:    "usage snapshot replaces token usage",
			payload: asyncTestEventWithUsage(asyncEventPromptUsageSnapshot, usage),
			prepare: func(app *App) {
				app.tokenUsage.ContextTokens = 123
			},
			assert: func(t *testing.T, app *App) {
				t.Helper()
				assert.Equal(t, 25, app.tokenUsage.ContextTokens)
			},
			canceledActive: false,
		},
		{
			name:    "canceled prompt still records late stream for preservation",
			payload: asyncTestEvent(asyncEventPromptDelta, "", asyncTestIgnored, 1),
			assert: func(t *testing.T, app *App) {
				t.Helper()
				require.Len(t, app.transcript.Streaming.Blocks, 1)
				assert.Equal(t, asyncTestIgnored, app.transcript.Streaming.Blocks[0].Content)
			},
			prepare:        nil,
			canceledActive: true,
		},
	}
}

func TestAsyncEventDataHelpers(t *testing.T) {
	t.Parallel()

	assert.Empty(t, promptDoneExtensionData(nil))
	responseData := promptDoneExtensionData(&assistant.PromptResponse{
		SessionID:        asyncTestSessionID,
		UserEntryID:      "user-entry",
		AssistantEntryID: "assistant-entry",
		Text:             "answer",
		Thinking:         nil,
		ToolEvents:       nil,
		Usage:            model.EmptyTokenUsage(),
		Cached:           true,
	})
	assert.Equal(t, asyncTestSessionID, responseData[extensionDataSessionID])
	assert.Equal(t, "answer", responseData[extensionDataText])
	assert.Equal(t, true, responseData[extensionDataCached])

	assert.Empty(t, toolExtensionData(nil))
	toolData := toolExtensionData(&assistant.ToolEvent{
		Name:          asyncTestToolStart,
		ArgumentsJSON: "{}",
		DetailsJSON:   "details",
		Result:        "ok",
		Error:         "",
		IsError:       false,
	})
	assert.Equal(t, asyncTestToolStart, toolData[extensionDataName])
	assert.Equal(t, "ok", toolData[extensionDataResult])
}

func asyncTestUsage() *model.TokenUsage {
	return &model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   25,
		InputTokens:     0,
		OutputTokens:    0,
	}
}

func asyncTestToolEvent() *assistant.ToolEvent {
	return &assistant.ToolEvent{
		Name:          asyncTestToolName,
		ArgumentsJSON: "{}",
		DetailsJSON:   "",
		Result:        "ok",
		Error:         "",
		IsError:       false,
	}
}

func asyncTestStreamEvent(
	kind assistant.StreamEventKind,
	text string,
	toolEvent *assistant.ToolEvent,
	usage *model.TokenUsage,
) assistant.StreamEvent {
	return assistant.StreamEvent{
		ToolCallEvent: nil,
		ToolEvent:     toolEvent,
		Usage:         usage,
		Kind:          kind,
		Text:          text,
	}
}

func asyncTestEvent(kind asyncEventKind, provider, text string, promptID uint64) *asyncEvent {
	return &asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          kind,
		Provider:      provider,
		Text:          text,
		PromptID:      promptID,
	}
}

func asyncTestEventWithTool(kind asyncEventKind, toolEvent *assistant.ToolEvent) *asyncEvent {
	event := asyncTestEvent(kind, "", "", 1)
	event.ToolEvent = toolEvent

	return event
}

func asyncTestEventWithUsage(kind asyncEventKind, usage *model.TokenUsage) *asyncEvent {
	event := asyncTestEvent(kind, "", "", 1)
	event.Usage = usage

	return event
}
