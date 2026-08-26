package database_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestWorkflowProgressBatchIsOrderedAndLeaseFenced(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	owner := fixture.createOwner(t.Context())
	run, err := fixture.workflows.Create(t.Context(), newWorkflowRun(owner.ID))
	require.NoError(t, err)

	claimed, err := fixture.workflows.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: run.Task.ID, LeaseOwner: testWorker, EventKind: "workflow_started",
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, claimed)

	drafts := []database.TaskEventDraft{
		{Kind: "workflow_event", PayloadJSON: `{"step":1}`},
		{Kind: "workflow_event", PayloadJSON: `{"step":2}`},
	}
	events, appended, err := fixture.workflows.Tasks().AppendRunningEvents(
		t.Context(), run.Task.ID, testWorker, drafts,
	)
	require.NoError(t, err)
	require.True(t, appended)
	require.Len(t, events, 2)
	assert.Equal(t, []int64{3, 4}, []int64{events[0].Sequence, events[1].Sequence})

	rejected, appended, err := fixture.workflows.Tasks().AppendRunningEvents(
		t.Context(), run.Task.ID, "stale-worker", drafts,
	)
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Empty(t, rejected)

	persisted, err := fixture.workflows.Tasks().ListEvents(t.Context(), run.Task.ID, 0, 10)
	require.NoError(t, err)
	assert.Len(t, persisted, 4, "queued + started + one committed batch")
}
