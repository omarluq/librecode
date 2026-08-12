package assistant_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
)

func TestRuntime_AutoCompactsOversizedRequestBeforeProviderCall(t *testing.T) {
	t.Parallel()

	harness := newAutoCompactionRuntimeHarness(t, []string{"summary of old context", "final answer"}, 16_000)
	ctx := context.Background()
	session, err := harness.runtime.SessionRepository().CreateSession(ctx, testRuntimeCWD, "auto compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(
		t,
		harness.runtime.SessionRepository(),
		session.ID,
		nil,
		database.RoleUser,
		strings.Repeat("old ", 15_000),
	)
	appendRuntimeTestMessage(
		t,
		harness.runtime.SessionRepository(),
		session.ID,
		&old.ID,
		database.RoleAssistant,
		"tail",
	)

	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID
	events := []assistant.StreamEvent{}
	request.OnEvent = func(event assistant.StreamEvent) {
		events = append(events, event)
	}

	response, err := harness.runtime.Prompt(ctx, request)

	require.NoError(t, err)
	assert.Equal(t, "final answer", response.Text)
	require.Len(t, harness.client.requests, 2)
	assert.True(t, harness.client.requests[0].DisableTools)
	assert.False(t, harness.client.requests[1].DisableTools)
	assert.Contains(t, harness.client.requests[1].Messages[0].Content, "summary of old context")
	assert.Equal(t, "continue", harness.client.requests[1].Messages[len(harness.client.requests[1].Messages)-1].Content)
	assert.Condition(t, func() bool {
		for _, event := range events {
			if isContextAutoCompactionEvent(&event) {
				return true
			}
		}

		return false
	})

	branch, err := harness.runtime.SessionRepository().Branch(ctx, session.ID, response.AssistantEntryID)
	require.NoError(t, err)

	roles := make([]database.EntryType, 0, len(branch))
	for index := range branch {
		roles = append(roles, branch[index].Type)
	}

	assert.Contains(t, roles, database.EntryTypeCompaction)
}

type steeringPreflightCompactionCompleter struct {
	summaryEntered     chan struct{}
	allowSummary       chan struct{}
	completionRequest  chan *assistant.CompletionRequest
	allowCheckpoint    chan struct{}
	checkpointMessages chan []llm.Message
}

func newSteeringPreflightCompactionCompleter() *steeringPreflightCompactionCompleter {
	return &steeringPreflightCompactionCompleter{
		summaryEntered:     make(chan struct{}),
		allowSummary:       make(chan struct{}),
		completionRequest:  make(chan *assistant.CompletionRequest, 1),
		allowCheckpoint:    make(chan struct{}),
		checkpointMessages: make(chan []llm.Message, 1),
	}
}

func (client *steeringPreflightCompactionCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.DisableTools {
		close(client.summaryEntered)

		select {
		case <-client.allowSummary:
			return testCompletionResult("summary made while steering was pending"), nil
		case <-ctx.Done():
			return nil, oops.In("assistant_test").Code("compaction_canceled").Wrapf(ctx.Err(), "wait for summary")
		}
	}

	client.completionRequest <- request

	return completeAfterCheckpoint(
		ctx, request, client.allowCheckpoint, client.checkpointMessages, "final answer",
	)
}

func completeAfterCheckpoint(
	ctx context.Context,
	request *assistant.CompletionRequest,
	allow <-chan struct{},
	checkpointMessages chan<- []llm.Message,
	text string,
) (*assistant.CompletionResult, error) {
	select {
	case <-allow:
	case <-ctx.Done():
		return nil, oops.In("assistant_test").Code("completion_canceled").Wrapf(ctx.Err(), "wait for checkpoint")
	}

	messages, err := request.OnRoundCheckpoint(ctx, &llm.CompletedRound{
		Assistant: llm.Message{
			Metadata: nil, Role: llm.RoleAssistant, Content: []llm.Part{llm.TextPart(text)},
		},
		ToolResults: nil, FinishReason: llm.FinishReasonStop, Usage: llm.EmptyUsage(),
	})
	if err != nil {
		return nil, oops.In("assistant_test").Code("checkpoint").Wrapf(err, "checkpoint completed round")
	}

	checkpointMessages <- messages

	return testCompletionResult(text), nil
}

func TestRuntime_PreflightCompactionDefersSteeringUntilRoundCheckpoint(t *testing.T) {
	t.Parallel()

	client := newSteeringPreflightCompactionCompleter()
	runtime := newAutoCompactionTestRuntime(t, client, 16_000)
	ctx := context.Background()
	session, err := runtime.SessionRepository().CreateSession(ctx, testRuntimeCWD, t.Name(), "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(
		t, runtime.SessionRepository(), session.ID, nil, database.RoleUser, strings.Repeat("old ", 15_000),
	)
	appendRuntimeTestMessage(t, runtime.SessionRepository(), session.ID, &old.ID, database.RoleAssistant, "tail")

	runStarted := make(chan assistant.PromptUserEntryEvent, 1)
	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID
	request.OnUserEntry = func(event assistant.PromptUserEntryEvent) { runStarted <- event }
	promptResult := make(chan error, 1)

	go func() {
		_, promptErr := runtime.Prompt(ctx, request)
		promptResult <- promptErr
	}()

	run := <-runStarted

	<-client.summaryEntered
	require.NoError(t, runtime.Steer(ctx, &assistant.SteeringRequest{
		SessionID: run.SessionID,
		RunID:     run.EntryID,
		Text:      "steer during compaction", Images: nil, HideUserPrompt: false,
	}))
	close(client.allowSummary)

	completionRequest := <-client.completionRequest
	for _, message := range completionRequest.Messages {
		assert.NotContains(t, message.Content, "steer during compaction",
			"steering accepted during compaction must wait for a completed-round checkpoint")
	}

	close(client.allowCheckpoint)
	checkpointMessages := <-client.checkpointMessages
	require.Len(t, checkpointMessages, 1)
	assert.Equal(t, "steer during compaction", checkpointMessages[0].Content[0].Text)
	require.NoError(t, <-promptResult)

	assertSteeringAfterCompactedRound(
		t, runtime, session.ID, "final answer", "steer during compaction",
	)
}

type autoCompactionRuntimeHarness struct {
	runtime *assistant.Runtime
	client  *recordingCompleter
}

func newAutoCompactionRuntimeHarness(
	t *testing.T,
	responses []string,
	contextWindow int,
) autoCompactionRuntimeHarness {
	t.Helper()

	client := newSequencedCompleter(responses...)
	runtime := newAutoCompactionTestRuntime(t, client, contextWindow)

	return autoCompactionRuntimeHarness{runtime: runtime, client: client}
}

func newAutoCompactionTestRuntime(
	t *testing.T,
	client assistant.Completer,
	contextWindow int,
) *assistant.Runtime {
	t.Helper()

	runtime := newTestRuntimeWithContextWindow(t, client, contextWindow)
	runtimeConfig := testConfig()
	runtimeConfig.Context.ProviderReserveTokens = 0
	runtimeConfig.Context.SafetyMarginTokens = 0
	// Reserve one token so post-response compaction tests keep a stable output headroom
	// and do not depend on off-by-one budget boundaries.
	runtimeConfig.Context.OutputReserveTokens = 1

	return assistant.NewRuntimeForTest(func(opts *assistant.RuntimeTestOptions) {
		opts.Config = runtimeConfig
		opts.Sessions = runtime.SessionRepository()
		opts.Cache = assistant.NewResponseCache(false, 1, time.Minute)
		opts.Models = runtime.ModelRegistry()
		opts.Client = client
	})
}

func isContextAutoCompactionEvent(event *assistant.StreamEvent) bool {
	return event.Kind == assistant.StreamEventContextCompactionDone &&
		strings.Contains(event.Text, "context auto-compacted")
}
