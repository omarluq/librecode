package database_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionRepositoryAppendCompactionRejectsNilInput(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)

	entry, err := repository.AppendCompaction(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, entry)
	assert.Contains(t, err.Error(), "append compaction input is nil")
}
