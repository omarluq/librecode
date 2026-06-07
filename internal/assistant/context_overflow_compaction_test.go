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

			client := newOverflowRecoveryCompletionClient(testCase.summary, testCase.final, nil)
			runtime := newProviderOverflowRecoveryRuntime(t, client)
			response, events, sessionID, err := runProviderOverflowPrompt(t, runtime, testCase.name)

			assert.Equal(t, []bool{false, true, false}, client.disableToolsByCall)
			if testCase.wantRetryErr {
				require.Nil(t, response)
				require.Error(t, err)
				assert.True(t, assistant.IsContextWindowError(err))
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

			response, _, _, err := runProviderOverflowPrompt(t, runtime, testCase.name)

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

	return response, events, session.ID, promptErr
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
