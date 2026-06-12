package tool

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectFindResultsRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := collectFindResults(ctx, root, "**/*", 10)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
