package assistant_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestRuntime_OverflowRecoveryFailureNeverReplaysProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		client        *recordingCompleter
		name          string
		contextWindow int
	}{
		{
			name:          "compaction provider failure",
			client:        newOverflowSummaryCompleter("", errors.New("summary failed")),
			contextWindow: 200_000,
		},
		{
			name: "rebuilt request fails preflight",
			client: newOverflowSummaryCompleter(
				structuredTestSummary(strings.Repeat("summary ", 30_000)), nil,
			),
			contextWindow: 20_000,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtime := newAutoCompactionTestRuntime(t, testCase.client, testCase.contextWindow)
			response, _, _, err := runProviderOverflowPrompt(t, runtime, testCase.name)

			require.Error(t, err)
			assert.Nil(t, response)
			assert.Equal(t, 1, providerRequestCount(testCase.client),
				"recovery failure must consume the one-shot recovery without replaying the provider")
		})
	}
}

func TestRuntime_OverflowCompactionPersistenceFailureNeverReplaysProvider(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(t.Context(), connection))
	repository := testutil.SessionRepository(t, connection)

	client := newOverflowSummaryCompleter("summary before persistence failure", nil)
	originalComplete := client.complete
	client.complete = func(call int, request *assistant.CompletionRequest) (*assistant.CompletionResult, error) {
		result, completeErr := originalComplete(call, request)
		if request.DisableTools {
			require.NoError(t, connection.Close())
		}

		return result, completeErr
	}

	cfg := testConfig()
	cfg.Context.ProviderReserveTokens = 0
	cfg.Context.SafetyMarginTokens = 0
	cfg.Context.OutputReserveTokens = 80
	cache := assistant.NewResponseCache(false, 1, time.Minute)
	t.Cleanup(cache.Shutdown)

	runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
		options.Config = cfg
		options.Sessions = repository
		options.Cache = cache
		options.Models = newCompactionTestRegistry(t, 64_000)
		options.Client = client
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	})

	response, _, _, promptErr := runProviderOverflowPrompt(t, runtime, t.Name())

	require.Error(t, promptErr)
	assert.Nil(t, response)
	assert.Equal(t, 1, providerRequestCount(client),
		"failed compaction persistence must not issue a recovered provider request")
}

func TestRuntime_PostResponseCompactionFailurePreservesCompletedResponse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	client := failingSummaryClient()
	runtime := newAutoCompactionErrorRuntimeWithWindow(t, client, 160_000)
	repository := runtime.SessionRepository()
	session, err := repository.CreateSession(context.Background(), testRuntimeCWD, t.Name(), "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(
		t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 150_000),
	)
	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID
	request.ParentEntryID = &old.ID

	response, err := runtime.Prompt(t.Context(), request)

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, autoCompactionTestFinalAnswer, response.Text)
	assert.Equal(t, 1, providerRequestCount(client))

	leaf, found, leafErr := repository.LeafEntry(t.Context(), session.ID)
	require.NoError(t, leafErr)
	require.True(t, found)
	assert.Equal(t, response.AssistantEntryID, leaf.ID)
	assert.Equal(t, database.RoleAssistant, leaf.Message.Role)
	assert.Equal(t, autoCompactionTestFinalAnswer, leaf.Message.Content)
}

func providerRequestCount(client *recordingCompleter) int {
	count := 0

	for _, request := range client.requests {
		if !request.DisableTools {
			count++
		}
	}

	return count
}
