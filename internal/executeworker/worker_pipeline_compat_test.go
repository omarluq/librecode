package executeworker_test

import (
	"testing"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/guestapi"
	"github.com/stretchr/testify/require"
)

func TestVersion1PipelinePreservesCallbackPanic(t *testing.T) {
	t.Parallel()

	bindings := executeworker.WorkflowModeBindings(nil)
	pipeline, ok := bindings[guestapi.PackageWorkflow]["Pipeline"].(func(
		[]any, func(any) (any, error), int,
	) (any, error))
	require.True(t, ok)

	const panicValue = "v1 callback panic"
	require.PanicsWithValue(t, panicValue, func() {
		result, err := pipeline([]any{1}, func(any) (any, error) {
			panic(panicValue)
		}, 1)
		require.NoError(t, err)
		require.Nil(t, result)
	})
}
