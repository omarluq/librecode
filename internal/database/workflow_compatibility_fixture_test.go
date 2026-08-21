package database_test

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/testutil"
)

const (
	compatibilityOwnerID = "01900000-0000-7000-8000-000000000001"
	compatibilityRunID   = "01900000-0000-7000-8000-000000000010"
	compatibilityNodeKey = "inspect"
)

// workflowCompatibilityV10 is a data-only export created against schema version 10,
// before later unrelated migrations. It freezes workflow, event, and replay-link shapes.
//
//go:embed testdata/workflow_compatibility_v10.sql
var workflowCompatibilityV10 string

func TestPersistedWorkflowCompatibilityFixture(t *testing.T) {
	t.Parallel()

	connection := newMigratedThroughVersion(t, 10)
	_, err := connection.ExecContext(t.Context(), workflowCompatibilityV10)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(t.Context(), connection))

	repository := testutil.WorkflowRepository(t, connection)
	run, found, err := repository.Get(t.Context(), compatibilityRunID)
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, database.TaskKindWorkflow, run.Task.Kind)
	assert.Equal(t, database.TaskSucceeded, run.Task.State)
	assert.Equal(t, compatibilityOwnerID, run.Task.OwnerSessionID)
	assert.Equal(t, "compatibility fixture", run.Name)
	assert.Equal(t, "v1", run.SourceVersion)
	assert.JSONEq(t, `{"scope":"terminal states"}`, run.ArgumentsJSON)

	links, err := repository.ListAgentTaskDetails(t.Context(), []string{compatibilityRunID})
	require.NoError(t, err)
	require.Len(t, links, 2)

	assert.Equal(t, []string{compatibilityNodeKey, compatibilityNodeKey}, []string{
		links[0].Link.NodeKey, links[1].Link.NodeKey,
	})
	assert.Equal(t, []int{0, 1}, []int{links[0].Link.InvocationIndex, links[1].Link.InvocationIndex})
	assert.Equal(t, []int64{1, 2}, []int64{links[0].Link.Sequence, links[1].Link.Sequence})
	assert.Equal(t, compatibilityOwnerID, links[0].AgentTask.Task.OwnerSessionID)
	assert.Equal(t, compatibilityRunID, links[0].AgentTask.Task.ParentTaskID)

	replayed, found, err := repository.FindAgentTask(t.Context(), compatibilityRunID, compatibilityNodeKey, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, links[1].AgentTask.Task.ID, replayed.AgentTaskID)

	events, err := repository.Tasks().ListEvents(t.Context(), compatibilityRunID, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, []int64{1, 2, 3}, []int64{events[0].Sequence, events[1].Sequence, events[2].Sequence})
	assert.Equal(t, []string{"task_queued", "task_started", "task_succeeded"}, []string{
		events[0].Event.Kind, events[1].Event.Kind, events[2].Event.Kind,
	})
}
