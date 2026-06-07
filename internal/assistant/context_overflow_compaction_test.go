package assistant_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

func TestRuntime_CompactsAndRetriesProviderContextOverflow(t *testing.T) {
	t.Parallel()

	client := newOverflowRecoveryCompletionClient(
		"summary after provider overflow",
		"recovered answer",
		nil,
	)
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
	assertContainsContextCompactionEvent(t, events, "attempting compaction before retry")
	assertContainsContextCompactionEvent(t, events, "context auto-compacted after provider overflow")

	branch, err := runtime.SessionRepository().Branch(ctx, session.ID, response.AssistantEntryID)
	require.NoError(t, err)
	assert.Contains(t, branchEntryTypes(branch), database.EntryTypeCompaction)
}

func TestRuntime_ProviderContextOverflowRetriesOnlyOnce(t *testing.T) {
	t.Parallel()

	client := newOverflowRecoveryCompletionClient(
		"summary after overflow",
		autoCompactionTestUnused,
		nil,
	)
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
	client := newOverflowRecoveryCompletionClient("", "", overflowErr)
	runtime := newProviderOverflowRecoveryRuntime(t, client)
	request := newRuntimePromptRequest(testRuntimeCWD, "short", "")

	response, err := runtime.Prompt(context.Background(), request)

	require.Nil(t, response)
	assert.ErrorIs(t, err, overflowErr)
	assert.Equal(t, []bool{false}, client.disableToolsByCall)
}

func TestRuntime_ProviderContextOverflowRecoveryErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		client        assistant.CompletionClient
		name          string
		wantCode      string
		contextWindow int
	}{
		{
			name:          "wraps compaction failure",
			client:        newOverflowSummaryCompletionClient("", errors.New("summary failed")),
			wantCode:      "context_overflow_compact",
			contextWindow: 200_000,
		},
		{
			name:          "wraps rebuilt budget failure",
			client:        newOverflowSummaryCompletionClient(strings.Repeat("summary ", 30_000), nil),
			wantCode:      "context_budget_after_provider_overflow_compact",
			contextWindow: 20_000,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtime := newAutoCompactionTestRuntime(t, testCase.client, testCase.contextWindow)
			repository := runtime.SessionRepository()
			session, err := repository.CreateSession(context.Background(), testRuntimeCWD, testCase.name, "")
			require.NoError(t, err)
			appendAutoCompactionOldTurn(t, repository, session.ID)
			request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
			request.SessionID = session.ID

			response, err := runtime.Prompt(context.Background(), request)

			require.Nil(t, response)
			requireOuterOopsCode(t, err, testCase.wantCode)
		})
	}
}

func TestRuntime_ProviderOverflowRecoveryInputGuards(t *testing.T) {
	t.Parallel()

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
			wantCode: "context_overflow_recovery_input",
		},
		{
			name: "nil nested input",
			call: func() error {
				return runtime.ProviderOverflowRecoveryNilBuildForTest(context.Background())
			},
			wantCode: "context_overflow_recovery_input",
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

func newProviderOverflowRecoveryRuntime(
	t *testing.T,
	client assistant.CompletionClient,
) *assistant.Runtime {
	t.Helper()

	return newAutoCompactionTestRuntime(t, client, 64_000)
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

type providerOverflowStaticErrorClient struct{}

func (providerOverflowStaticErrorClient) Complete(
	context.Context,
	*assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	return nil, errors.New("provider exploded")
}
