package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntime_UsableCachedResponseWithoutSteeringRegistry(t *testing.T) {
	t.Parallel()

	runtime := newRuntimeFromDeps(nil)
	runtime.steering = nil
	input := &responseInput{
		lineage:          newPromptLineage("run-1"),
		onEvent:          nil,
		onRetry:          nil,
		sessionID:        adapterSessionID,
		cwd:              "",
		prompt:           "",
		hasPromptImages:  false,
		contextHasImages: false,
	}

	bundle, usable, err := runtime.usableCachedResponse(input, "cached answer")

	require.NoError(t, err)
	assert.True(t, usable)
	require.NotNil(t, bundle)
	assert.Equal(t, "cached answer", bundle.Text)
	assert.True(t, bundle.ModelFacing)
}
