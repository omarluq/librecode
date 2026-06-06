package assistant_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func TestRuntime_CompactsAndRetriesProviderContextOverflow(t *testing.T) {
	t.Parallel()

	client := &providerOverflowRecoveryClient{
		overflowErr:        nil,
		requests:           nil,
		disableToolsByCall: nil,
		summary:            "summary after provider overflow",
		final:              "recovered answer",
	}
	runtime := newProviderOverflowRecoveryRuntime(t, client)
	ctx := context.Background()
	session, err := runtime.SessionRepository().CreateSession(ctx, testRuntimeCWD, "overflow", "")
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
	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID
	events := []assistant.StreamEvent{}
	request.OnEvent = func(event assistant.StreamEvent) {
		events = append(events, event)
	}

	response, err := runtime.Prompt(ctx, request)

	require.NoError(t, err)
	assert.Equal(t, "recovered answer", response.Text)
	assert.Equal(t, []bool{false, true, false}, client.disableToolsByCall)
	require.Len(t, client.requests, 3)
	assert.Contains(t, client.requests[2].Messages[0].Content, "summary after provider overflow")
	assert.Contains(t, client.requests[2].Messages[len(client.requests[2].Messages)-1].Content, "continue")
	assertContainsContextCompactionEvent(t, events, "provider reported context overflow")
	assertContainsContextCompactionEvent(t, events, "context auto-compacted after provider overflow")

	branch, err := runtime.SessionRepository().Branch(ctx, session.ID, response.AssistantEntryID)
	require.NoError(t, err)
	assert.Contains(t, branchEntryTypes(branch), database.EntryTypeCompaction)
}

func TestRuntime_ProviderContextOverflowRetriesOnlyOnce(t *testing.T) {
	t.Parallel()

	client := &providerOverflowRecoveryClient{
		overflowErr:        nil,
		requests:           nil,
		disableToolsByCall: nil,
		summary:            "summary after overflow",
		final:              autoCompactionTestUnused,
	}
	runtime := newProviderOverflowRecoveryRuntime(t, client)
	ctx := context.Background()
	session, err := runtime.SessionRepository().CreateSession(ctx, testRuntimeCWD, "overflow once", "")
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
	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID

	response, err := runtime.Prompt(ctx, request)

	require.Nil(t, response)
	require.Error(t, err)
	assert.True(t, assistant.IsContextWindowError(err))
	assert.Equal(t, []bool{false, true, false}, client.disableToolsByCall)
}

func TestRuntime_ProviderContextOverflowPreservesOriginalErrorWhenNoCompaction(t *testing.T) {
	t.Parallel()

	overflowErr := errors.New("Your input exceeds the context window of this model")
	client := &providerOverflowRecoveryClient{
		overflowErr:        overflowErr,
		requests:           nil,
		disableToolsByCall: nil,
		summary:            "",
		final:              "",
	}
	runtime := newProviderOverflowRecoveryRuntime(t, client)
	request := newRuntimePromptRequest(testRuntimeCWD, "short", "")

	response, err := runtime.Prompt(context.Background(), request)

	require.Nil(t, response)
	assert.ErrorIs(t, err, overflowErr)
	assert.Equal(t, []bool{false}, client.disableToolsByCall)
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
			err:  oops.In("assistant").Code("context_window_exceeded").Errorf("preflight failed"),
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

func newProviderOverflowRecoveryRuntime(
	t *testing.T,
	client assistant.CompletionClient,
) *assistant.Runtime {
	t.Helper()

	runtime := newTestRuntimeWithContextWindow(t, client, 64_000)
	runtimeConfig := testConfig()
	runtimeConfig.Context.KeepRecentTokens = 1
	runtimeConfig.Context.ProviderReserveTokens = 0
	runtimeConfig.Context.SafetyMarginTokens = 0
	runtimeConfig.Context.OutputReserveTokens = 1

	return assistant.NewRuntime(
		runtimeConfig,
		runtime.SessionRepository(),
		nil,
		assistant.NewResponseCache(false, 1, time.Minute),
		runtime.EventBus(),
		runtime.ModelRegistry(),
		client,
		nil,
	)
}

func assertContainsContextCompactionEvent(t *testing.T, events []assistant.StreamEvent, text string) {
	t.Helper()

	for index := range events {
		if events[index].Kind == assistant.StreamEventContextCompaction && strings.Contains(events[index].Text, text) {
			return
		}
	}

	t.Fatalf("expected context compaction event containing %q", text)
}

func branchEntryTypes(branch []database.EntryEntity) []database.EntryType {
	types := make([]database.EntryType, 0, len(branch))
	for index := range branch {
		types = append(types, branch[index].Type)
	}

	return types
}

//nolint:govet // Test fixture readability matters more than field packing.
type providerOverflowRecoveryClient struct {
	overflowErr        error
	requests           []*assistant.CompletionRequest
	disableToolsByCall []bool
	summary            string
	final              string
}

func (client *providerOverflowRecoveryClient) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.requests = append(client.requests, request)
	client.disableToolsByCall = append(client.disableToolsByCall, request.DisableTools)
	if request.DisableTools {
		return providerOverflowCompletionResult(client.summary), nil
	}
	switch len(client.disableToolsByCall) {
	case 1:
		if client.overflowErr != nil {
			return nil, client.overflowErr
		}

		return nil, oops.In("assistant").Code("responses_status").Errorf("maximum context length exceeded")
	case 3:
		if client.final == autoCompactionTestUnused {
			return nil, oops.In("assistant").Code("responses_status").Errorf("maximum context length exceeded")
		}
	}

	return providerOverflowCompletionResult(client.final), nil
}

func providerOverflowCompletionResult(text string) *assistant.CompletionResult {
	return &assistant.CompletionResult{
		Text:       text,
		Thinking:   nil,
		ToolEvents: nil,
		Usage:      model.EmptyTokenUsage(),
	}
}
