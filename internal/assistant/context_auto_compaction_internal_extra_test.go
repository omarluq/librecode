package assistant_test

import (
	"context"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

func TestRuntime_AutoCompactionBeforeRequestErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		client   assistant.Completer
		seed     func(t *testing.T, repository *database.SessionRepository, sessionID string)
		name     string
		wantCode string
	}{
		{
			name:     "preserves validation error when nothing can be compacted",
			client:   newSequencedCompleter(autoCompactionTestUnused),
			seed:     nil,
			wantCode: testContextWindowExceededOopsCode,
		},
		{
			name:   "wraps summarization failure",
			client: failingSummaryClient(),
			seed: func(t *testing.T, repository *database.SessionRepository, sessionID string) {
				t.Helper()
				appendAutoCompactionOldTurn(t, repository, sessionID)
			},
			wantCode: "compact_summarize",
		},
		{
			name:   "wraps rebuilt budget failure",
			client: largeSummaryClient(200),
			seed: func(t *testing.T, repository *database.SessionRepository, sessionID string) {
				t.Helper()
				appendAutoCompactionOldTurn(t, repository, sessionID)
			},
			wantCode: testContextWindowExceededOopsCode,
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
	t.Setenv("HOME", t.TempDir())

	runtime := newAutoCompactionErrorRuntimeWithWindow(t, failingSummaryClient(), 160_000)
	repository := runtime.SessionRepository()
	session, err := repository.CreateSession(context.Background(), testRuntimeCWD, "post error", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 150_000))
	oldAssistant := appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "recent tail")
	events := []assistant.StreamEvent{}

	runtime.AutoCompactAfterResponseForTest(context.Background(), func(event assistant.StreamEvent) {
		events = append(events, event)
	}, session.ID, testRuntimeCWD, oldAssistant.ID)

	assertContainsCompactionErrorEvent(t, events)
}

func assertContainsCompactionErrorEvent(t *testing.T, events []assistant.StreamEvent) {
	t.Helper()

	for _, event := range events {
		if event.Kind == assistant.StreamEventContextCompactionError &&
			strings.Contains(event.Text, "context auto-compaction after response failed:") {
			return
		}
	}

	t.Error("expected context compaction error event not found")
}

func TestRuntime_EmitPostResponseAutoCompactionErrorSkipsNil(t *testing.T) {
	t.Parallel()

	client := newSummaryAwareCompleter("summary", nil, autoCompactionTestFinalAnswer)
	runtime := newAutoCompactionErrorRuntime(t, client)
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

func newAutoCompactionErrorRuntime(t *testing.T, client assistant.Completer) *assistant.Runtime {
	t.Helper()

	return newAutoCompactionErrorRuntimeWithWindow(t, client, 64)
}

func newAutoCompactionErrorRuntimeWithWindow(
	t *testing.T,
	client assistant.Completer,
	contextWindow int,
) *assistant.Runtime {
	t.Helper()

	return newAutoCompactionTestRuntime(t, client, contextWindow)
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

	oopsErr := requireOopsError(t, err)
	require.Equal(t, wantCode, oopsErr.Code())
}

func requireOuterOopsCode(t *testing.T, err error, wantCode string) {
	t.Helper()

	oopsErr := requireOopsError(t, err)
	layers := oopsErr.Layers()
	require.NotEmpty(t, layers)
	require.Equal(t, wantCode, layers[0].Code)
}

func requireOopsError(t *testing.T, err error) oops.OopsError {
	t.Helper()

	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)

	return oopsErr
}
