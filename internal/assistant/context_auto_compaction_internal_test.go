// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
)

func TestRuntime_PrepareCompletionRequestWithAutoCompactionRejectsNilInput(t *testing.T) {
	t.Parallel()

	runtime := runtimeWithoutDependencies()
	build, compactionEntry, err := runtime.prepareCompletionRequestWithAutoCompaction(context.Background(), nil)

	require.Nil(t, build)
	require.Nil(t, compactionEntry)
	requirePrepareInputError(t, err)
}

func TestRuntime_PrepareCompletionRequestWithAutoCompactionRejectsNilAuth(t *testing.T) {
	t.Parallel()

	runtime := runtimeWithoutDependencies()
	build, compactionEntry, err := runtime.prepareCompletionRequestWithAutoCompaction(
		context.Background(),
		&completionRequestPreparationInput{
			selectedModel: nil,
			onEvent:       nil,
			auth:          nil,
			sessionID:     "",
			cwd:           "",
			prompt:        "",
			userEntryID:   "",
		},
	)

	require.Nil(t, build)
	require.Nil(t, compactionEntry)
	requirePrepareInputError(t, err)
}

func runtimeWithoutDependencies() Runtime {
	return Runtime{
		cfg:        nil,
		sessions:   nil,
		extensions: nil,
		cache:      nil,
		events:     nil,
		models:     nil,
		client:     nil,
		logger:     nil,
	}
}

func requirePrepareInputError(t *testing.T, err error) {
	t.Helper()

	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	require.Equal(t, "context_prepare_input", oopsErr.Code())
}
