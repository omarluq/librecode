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

const (
	autoCompactionTestFinalAnswer = "final answer"
	autoCompactionTestUnused      = "unused"
)

func TestRuntime_AutoCompactionBeforeRequestErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		client   assistant.CompletionClient
		seed     func(t *testing.T, repository *database.SessionRepository, sessionID string)
		name     string
		wantCode string
	}{
		{
			name:     "preserves validation error when nothing can be compacted",
			client:   autoCompactionValidationOnlyClient{},
			seed:     nil,
			wantCode: "context_window_exceeded",
		},
		{
			name:   "wraps summarization failure",
			client: autoCompactionFailingSummaryClient{},
			seed: func(t *testing.T, repository *database.SessionRepository, sessionID string) {
				t.Helper()
				appendAutoCompactionOldTurn(t, repository, sessionID)
			},
			wantCode: "compact_summarize",
		},
		{
			name:   "wraps rebuilt budget failure",
			client: autoCompactionLargeSummaryClient{},
			seed: func(t *testing.T, repository *database.SessionRepository, sessionID string) {
				t.Helper()
				appendAutoCompactionOldTurn(t, repository, sessionID)
			},
			wantCode: "context_window_exceeded",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtime := newAutoCompactionErrorRuntime(t, testCase.client)
			repository := runtime.SessionRepository()
			session, err := repository.CreateSession(context.Background(), testRuntimeCWD, testCase.name, "")
			require.NoError(t, err)
			if testCase.seed != nil {
				testCase.seed(t, repository, session.ID)
			}
			request := newRuntimePromptRequest(testRuntimeCWD, strings.Repeat("prompt ", 200), "")
			request.SessionID = session.ID

			response, err := runtime.Prompt(context.Background(), request)

			require.Nil(t, response)
			requireOopsCode(t, err, testCase.wantCode)
		})
	}
}

func TestRuntime_AutoCompactionAfterResponseErrorEvent(t *testing.T) {
	t.Parallel()

	runtime := newAutoCompactionErrorRuntimeWithWindow(t, autoCompactionFailingSummaryClient{}, 30_000)
	repository := runtime.SessionRepository()
	session, err := repository.CreateSession(context.Background(), testRuntimeCWD, "post error", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 20_000))
	oldAssistant := appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "recent tail")
	events := []assistant.StreamEvent{}

	runtime.AutoCompactAfterResponseForTest(context.Background(), func(event assistant.StreamEvent) {
		events = append(events, event)
	}, session.ID, testRuntimeCWD, oldAssistant.ID)

	assert.Condition(t, func() bool {
		for _, event := range events {
			if event.Kind == assistant.StreamEventContextCompaction &&
				strings.Contains(event.Text, "context auto-compaction after response failed:") {
				return true
			}
		}

		return false
	})
}

func TestRuntime_EmitPostResponseAutoCompactionErrorSkipsNil(t *testing.T) {
	t.Parallel()

	runtime := newAutoCompactionErrorRuntime(t, autoCompactionStaticSummaryClient{})
	events := []assistant.StreamEvent{}

	runtime.EmitPostResponseAutoCompactionErrorForTest(context.Background(), func(event assistant.StreamEvent) {
		events = append(events, event)
	}, nil)

	assert.Empty(t, events)
}

func TestRuntime_ContextCompactionEventWithoutEntryDetails(t *testing.T) {
	t.Parallel()

	message := assistant.AutoCompactionMessageForTest(nil)

	assert.Contains(t, message, "context auto-compacted")
	assert.NotContains(t, message, "summarized")
	assert.NotContains(t, message, "kept recent context")
}

func newAutoCompactionErrorRuntime(t *testing.T, client assistant.CompletionClient) *assistant.Runtime {
	t.Helper()

	return newAutoCompactionErrorRuntimeWithWindow(t, client, 64)
}

func newAutoCompactionErrorRuntimeWithWindow(
	t *testing.T,
	client assistant.CompletionClient,
	contextWindow int,
) *assistant.Runtime {
	t.Helper()

	runtime := newTestRuntimeWithContextWindow(t, client, contextWindow)
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

func appendAutoCompactionOldTurn(t *testing.T, repository *database.SessionRepository, sessionID string) {
	t.Helper()

	first := appendRuntimeTestMessage(
		t,
		repository,
		sessionID,
		nil,
		database.RoleUser,
		strings.Repeat("old ", 120),
	)
	appendRuntimeTestMessage(
		t,
		repository,
		sessionID,
		&first.ID,
		database.RoleAssistant,
		strings.Repeat("older ", 120),
	)
}

func requireOopsCode(t *testing.T, err error, wantCode string) {
	t.Helper()

	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	require.Equal(t, wantCode, oopsErr.Code())
}

type autoCompactionValidationOnlyClient struct{}

func (autoCompactionValidationOnlyClient) Complete(
	context.Context,
	*assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	return autoCompactionResult(autoCompactionTestUnused), nil
}

type autoCompactionFailingSummaryClient struct{}

func (autoCompactionFailingSummaryClient) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.DisableTools {
		return nil, errors.New("summary failed")
	}

	return autoCompactionResult(autoCompactionTestFinalAnswer), nil
}

type autoCompactionLargeSummaryClient struct{}

func (autoCompactionLargeSummaryClient) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.DisableTools {
		return autoCompactionResult(strings.Repeat("summary ", 200)), nil
	}

	return autoCompactionResult(autoCompactionTestFinalAnswer), nil
}

type autoCompactionStaticSummaryClient struct{}

func (autoCompactionStaticSummaryClient) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.DisableTools {
		return autoCompactionResult("summary"), nil
	}

	return autoCompactionResult(autoCompactionTestFinalAnswer), nil
}

func autoCompactionResult(text string) *assistant.CompletionResult {
	return &assistant.CompletionResult{
		Thinking:   nil,
		ToolEvents: nil,
		Text:       text,
		Usage:      model.EmptyTokenUsage(),
	}
}
