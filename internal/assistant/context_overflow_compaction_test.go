package assistant_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
)

func TestRuntime_ProviderContextOverflowRecoveryScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		summary      string
		final        string
		wantText     string
		wantRetryErr bool
	}{
		{
			name:         "compacts and retries successfully",
			summary:      "summary after provider overflow",
			final:        "recovered answer",
			wantText:     "recovered answer",
			wantRetryErr: false,
		},
		{
			name:         "retries only once",
			summary:      "summary after overflow",
			final:        autoCompactionTestUnused,
			wantText:     "",
			wantRetryErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := newOverflowRecoveryCompleter(testCase.summary, testCase.final, nil)
			runtime := newProviderOverflowRecoveryRuntime(t, client)
			response, events, sessionID, err := runProviderOverflowPrompt(t, runtime, testCase.name)

			assert.Equal(t, []bool{false, true, false}, client.disableToolsByCall)

			if testCase.wantRetryErr {
				require.Nil(t, response)
				require.Error(t, err)
				assert.True(t, assistant.IsContextWindowError(err))

				leaf, found, leafErr := runtime.SessionRepository().LeafEntry(context.Background(), sessionID)
				require.NoError(t, leafErr)
				require.True(t, found)
				assertBranchContainsCompaction(t, runtime, sessionID, leaf.ID)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			assert.Equal(t, testCase.wantText, response.Text)
			require.Len(t, client.requests, 3)
			assert.Contains(t, client.requests[2].Messages[0].Content, testCase.summary)
			assert.Contains(t, client.requests[2].Messages[len(client.requests[2].Messages)-1].Content, "continue")
			assertContainsContextCompactionEvent(t, events, "attempting compaction before retry")
			assertContainsContextCompactionEvent(t, events, "context auto-compacted after provider overflow")
			assertBranchContainsCompaction(t, runtime, sessionID, response.AssistantEntryID)
		})
	}
}

type steeringOverflowCompactionCompleter struct {
	summaryEntered     chan struct{}
	allowSummary       chan struct{}
	recoveredRequest   chan *assistant.CompletionRequest
	allowCheckpoint    chan struct{}
	checkpointMessages chan []llm.Message
	summaryEnteredOnce sync.Once
	providerCalls      int
}

func newSteeringOverflowCompactionCompleter() *steeringOverflowCompactionCompleter {
	return &steeringOverflowCompactionCompleter{
		summaryEntered:     make(chan struct{}),
		summaryEnteredOnce: sync.Once{},
		allowSummary:       make(chan struct{}),
		recoveredRequest:   make(chan *assistant.CompletionRequest, 1),
		allowCheckpoint:    make(chan struct{}),
		checkpointMessages: make(chan []llm.Message, 1),
		providerCalls:      0,
	}
}

func (client *steeringOverflowCompactionCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.DisableTools {
		client.summaryEnteredOnce.Do(func() { close(client.summaryEntered) })

		select {
		case <-client.allowSummary:
			return testCompletionResult(structuredTestSummary("steering pending")), nil
		case <-ctx.Done():
			return nil, oops.In("assistant_test").Code("compaction_canceled").Wrapf(ctx.Err(), "wait for summary")
		}
	}

	client.providerCalls++
	if client.providerCalls == 1 {
		return nil, testContextWindowError()
	}

	select {
	case client.recoveredRequest <- request:
	case <-ctx.Done():
		return nil, oops.In("assistant_test").Code("completion_canceled").Wrapf(ctx.Err(), "publish recovered request")
	}

	return completeAfterCheckpoint(
		ctx, request, client.allowCheckpoint, client.checkpointMessages, "recovered round",
	)
}

func TestRuntime_ProviderOverflowSteeringWaitsForRecoveredRoundCheckpoint(t *testing.T) {
	t.Parallel()

	client := newSteeringOverflowCompactionCompleter()
	runtime := newProviderOverflowRecoveryRuntime(t, client)
	ctx := context.Background()
	session, err := runtime.SessionRepository().CreateSession(ctx, testRuntimeCWD, t.Name(), "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(
		t, runtime.SessionRepository(), session.ID, nil, database.RoleUser, strings.Repeat("old ", 1_000),
	)
	appendRuntimeTestMessage(t, runtime.SessionRepository(), session.ID, &old.ID, database.RoleAssistant, "tail")

	runStarted := make(chan assistant.PromptUserEntryEvent, 1)
	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID
	request.OnUserEntry = func(event assistant.PromptUserEntryEvent) { runStarted <- event }
	promptResult := make(chan struct {
		response *assistant.PromptResponse
		err      error
	}, 1)

	go func() {
		response, promptErr := runtime.Prompt(ctx, request)
		promptResult <- struct {
			response *assistant.PromptResponse
			err      error
		}{response: response, err: promptErr}
	}()

	run := <-runStarted

	select {
	case <-client.summaryEntered:
	case result := <-promptResult:
		require.NoError(t, result.err)
		t.Fatal("prompt completed before entering overflow compaction summary")
	}

	require.NoError(t, runtime.Steer(ctx, &assistant.SteeringRequest{
		SessionID:      run.SessionID,
		RunID:          run.EntryID,
		Text:           "steer after overflow",
		Images:         nil,
		HideUserPrompt: false,
	}))
	close(client.allowSummary)

	var recoveredRequest *assistant.CompletionRequest
	select {
	case recoveredRequest = <-client.recoveredRequest:
	case result := <-promptResult:
		require.NoError(t, result.err)
		t.Fatal("prompt completed before recovered model completion")
	}

	for _, message := range recoveredRequest.Messages {
		assert.NotContains(t, message.Content, "steer after overflow",
			"steering accepted during compaction must not alter the recovered retry request")
	}

	close(client.allowCheckpoint)

	checkpointMessages := <-client.checkpointMessages
	require.Len(t, checkpointMessages, 1)
	assert.Equal(t, "steer after overflow", checkpointMessages[0].Content[0].Text)

	result := <-promptResult
	require.NoError(t, result.err)
	require.NotNil(t, result.response)

	assertSteeringAfterCompactedRound(t, runtime, session.ID, "recovered round", "steer after overflow")
}

func assertSteeringAfterCompactedRound(
	t *testing.T,
	runtime *assistant.Runtime,
	sessionID string,
	completedRound string,
	steering string,
) {
	t.Helper()

	leaf, found, err := runtime.SessionRepository().LeafEntry(t.Context(), sessionID)
	require.NoError(t, err)
	require.True(t, found)

	branch, err := runtime.SessionRepository().Branch(t.Context(), sessionID, leaf.ID)
	require.NoError(t, err)

	compactionIndex := -1
	recoveredRoundIndex := -1
	steeringIndex := -1

	for index := range branch {
		switch {
		case branch[index].Type == database.EntryTypeCompaction:
			compactionIndex = index
		case recoveredRoundIndex == -1 && branch[index].Message.Role == database.RoleAssistant &&
			branch[index].Message.Content == completedRound:
			recoveredRoundIndex = index
		case branch[index].Message.Role == database.RoleUser && branch[index].Message.Content == steering:
			steeringIndex = index
		}
	}

	require.NotEqual(t, -1, compactionIndex)
	require.Greater(t, recoveredRoundIndex, compactionIndex)
	require.Greater(t, steeringIndex, recoveredRoundIndex)
}

func TestRuntime_ProviderContextOverflowDoesNotRecoverAfterToolExecutionStarts(t *testing.T) {
	t.Parallel()

	client := &recordingCompleter{
		complete: func(_ int, request *assistant.CompletionRequest) (*assistant.CompletionResult, error) {
			_, err := request.ExecuteTools(context.Background(), nil, nil)
			require.NoError(t, err)

			return nil, testContextWindowError()
		},
		requests:           nil,
		disableToolsByCall: nil,
	}
	runtime := newProviderOverflowRecoveryRuntime(t, client)

	response, _, _, err := runProviderOverflowPrompt(t, runtime, t.Name())

	require.Nil(t, response)
	require.Error(t, err)
	assert.True(t, assistant.IsContextWindowError(err))
	assert.Len(t, client.requests, 1)
}

func TestRuntime_ProviderContextOverflowPreservesOriginalErrorWhenNoCompaction(t *testing.T) {
	t.Parallel()

	overflowErr := errors.New("Your input exceeds the context window of this model")
	client := newOverflowRecoveryCompleter("", "", overflowErr)
	runtime := newProviderOverflowRecoveryRuntime(t, client)
	request := newRuntimePromptRequest(testRuntimeCWD, "short", "")

	response, err := runtime.Prompt(context.Background(), request)

	require.Nil(t, response)
	require.ErrorIs(t, err, overflowErr)
	assert.Equal(t, []bool{false}, client.disableToolsByCall)
}

func TestRuntime_ProviderContextOverflowRecoveryErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		client        assistant.Completer
		name          string
		wantCode      string
		contextWindow int
	}{
		{
			name:          "wraps compaction failure",
			client:        newOverflowSummaryCompleter("", errors.New("summary failed")),
			wantCode:      "context_overflow_compact",
			contextWindow: 200_000,
		},
		{
			name: "wraps rebuilt budget failure",
			client: newOverflowSummaryCompleter(
				structuredTestSummary(strings.Repeat("summary ", 30_000)), nil,
			),
			wantCode:      "context_overflow_compact",
			contextWindow: 20_000,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtime := newAutoCompactionTestRuntime(t, testCase.client, testCase.contextWindow)

			response, _, _, err := runProviderOverflowPrompt(t, runtime, testCase.name)

			require.Nil(t, response)
			requireOuterOopsCode(t, err, testCase.wantCode)
		})
	}
}

func TestRuntime_ProviderOverflowRecoveryInputGuards(t *testing.T) {
	t.Parallel()

	const recoveryInputCode = "context_overflow_recovery_input"

	runtime := newProviderOverflowRecoveryRuntime(t, providerOverflowStaticErrorClient{})
	tests := []struct {
		call     func() error
		name     string
		wantCode string
	}{
		{
			name: "nil input",
			call: func() error {
				return runtime.ProviderOverflowRecoveryNilInputForTest(context.Background())
			},
			wantCode: recoveryInputCode,
		},
		{
			name: "nil nested input",
			call: func() error {
				return runtime.ProviderOverflowRecoveryNilBuildForTest(context.Background())
			},
			wantCode: recoveryInputCode,
		},
		{
			name: "nil request",
			call: func() error {
				return runtime.ProviderOverflowRecoveryNilRequestForTest(context.Background())
			},
			wantCode: recoveryInputCode,
		},
		{
			name: "nil lineage",
			call: func() error {
				return runtime.ProviderOverflowRecoveryNilLineageForTest(context.Background())
			},
			wantCode: recoveryInputCode,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireOopsCode(t, testCase.call(), testCase.wantCode)
		})
	}
}

func TestRuntime_ProviderOverflowRecoveryPassesThroughNonContextErrors(t *testing.T) {
	t.Parallel()

	runtime := newProviderOverflowRecoveryRuntime(t, providerOverflowStaticErrorClient{})

	err := runtime.ProviderOverflowRecoveryNonContextErrorForTest(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider exploded")
}

func TestIsContextWindowError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
		want bool
	}{
		{
			name: "oops context code",
			err:  oops.In("assistant").Code(testContextWindowExceededOopsCode).Errorf("preflight failed"),
			want: true,
		},
		{
			name: "provider context window message",
			err:  errors.New("Your input exceeds the context window of this model"),
			want: true,
		},
		{
			name: "provider maximum context message",
			err:  errors.New("maximum context length exceeded"),
			want: true,
		},
		{
			name: "too many tokens message",
			err:  errors.New("too many tokens in request"),
			want: true,
		},
		{
			name: "request token limit message",
			err:  errors.New("token limit exceeded for request"),
			want: true,
		},
		{
			name: "daily token quota message",
			err:  errors.New("daily token limit exceeded"),
			want: false,
		},
		{
			name: "billing quota message",
			err:  errors.New("quota exceeded; update billing"),
			want: false,
		},
		{
			name: "rate limit",
			err:  errors.New("rate limit exceeded"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, assistant.IsContextWindowError(testCase.err))
		})
	}
}

func runProviderOverflowPrompt(
	t *testing.T,
	runtime *assistant.Runtime,
	name string,
) (*assistant.PromptResponse, []assistant.StreamEvent, string, error) {
	t.Helper()

	session, err := runtime.SessionRepository().CreateSession(context.Background(), testRuntimeCWD, name, "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(
		t,
		runtime.SessionRepository(),
		session.ID,
		nil,
		database.RoleUser,
		strings.Repeat("old ", 1_000),
	)
	appendRuntimeTestMessage(t, runtime.SessionRepository(), session.ID, &old.ID, database.RoleAssistant, "tail")

	events := []assistant.StreamEvent{}
	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID
	request.OnEvent = func(event assistant.StreamEvent) {
		events = append(events, event)
	}

	response, promptErr := runtime.Prompt(context.Background(), request)
	if promptErr != nil {
		return response, events, session.ID, fmt.Errorf("prompt with overflow compaction: %w", promptErr)
	}

	return response, events, session.ID, nil
}

func newProviderOverflowRecoveryRuntime(
	t *testing.T,
	client assistant.Completer,
) *assistant.Runtime {
	t.Helper()

	return newAutoCompactionTestRuntime(t, client, 64_000)
}

func assertContainsContextCompactionEvent(t *testing.T, events []assistant.StreamEvent, text string) {
	t.Helper()

	for index := range events {
		if isContextCompactionLifecycleEvent(events[index].Kind) && strings.Contains(events[index].Text, text) {
			return
		}
	}

	t.Fatalf("expected context compaction event containing %q", text)
}

func isContextCompactionLifecycleEvent(kind assistant.StreamEventKind) bool {
	switch kind {
	case assistant.StreamEventContextCompaction,
		assistant.StreamEventContextCompactionStart,
		assistant.StreamEventContextCompactionDone,
		assistant.StreamEventContextCompactionError:
		return true
	case assistant.StreamEventTextDelta,
		assistant.StreamEventThinkingDelta,
		assistant.StreamEventToolStart,
		assistant.StreamEventToolResult,
		assistant.StreamEventSkillLoaded,
		assistant.StreamEventUsage,
		assistant.StreamEventUsageSnapshot,
		assistant.StreamEventUsageTotal,
		assistant.StreamEventSteeringConsumed,
		assistant.StreamEventUnknown:
		return false
	}

	return false
}

func assertBranchContainsCompaction(
	t *testing.T,
	runtime *assistant.Runtime,
	sessionID string,
	leafID string,
) {
	t.Helper()

	branch, err := runtime.SessionRepository().Branch(context.Background(), sessionID, leafID)
	require.NoError(t, err)

	for index := range branch {
		if branch[index].Type == database.EntryTypeCompaction {
			return
		}
	}

	t.Fatal("expected branch to contain compaction entry")
}

type providerOverflowStaticErrorClient struct{}

func (providerOverflowStaticErrorClient) Complete(
	context.Context,
	*assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	return nil, errors.New("provider exploded")
}
