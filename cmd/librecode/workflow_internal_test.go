package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestWorkflowCommandRegistersSubmit(t *testing.T) {
	t.Parallel()

	submit, _, err := newWorkflowCmd().Find([]string{"submit"})
	require.NoError(t, err)
	assert.Equal(t, "submit <source-file>", submit.Use)
	require.Error(t, submit.Args(submit, nil))
	require.NoError(t, submit.Args(submit, []string{"workflow.mvm"}))
}

func TestWorkflowCommandRegistersWorker(t *testing.T) {
	t.Parallel()

	worker, _, err := newWorkflowCmd().Find([]string{"worker"})
	require.NoError(t, err)
	assert.Equal(t, "worker", worker.Use)
	require.NoError(t, worker.Args(worker, nil))
	require.Error(t, worker.Args(worker, []string{"unexpected"}))
}

func TestWorkflowCommandRegistersMetrics(t *testing.T) {
	t.Parallel()

	metrics, _, err := newWorkflowCmd().Find([]string{"metrics"})
	require.NoError(t, err)
	assert.Equal(t, "metrics <run-id>", metrics.Use)
	require.Error(t, metrics.Args(metrics, nil))
	require.NoError(t, metrics.Args(metrics, []string{"run-id"}))
}

func TestTaskElapsed(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	started := created.Add(time.Second)
	finished := started.Add(3 * time.Second)
	assert.Equal(t, 3*time.Second, taskElapsed(&database.TaskEntity{
		LeaseExpiresAt: nil, ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: "", ConcurrencyKey: "",
		LeaseOwner: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
		CreatedAt: created, StartedAt: &started, FinishedAt: &finished, UpdatedAt: finished,
	}))
}

func TestWorkflowCommandRegistersResume(t *testing.T) {
	t.Parallel()

	resume, _, err := newWorkflowCmd().Find([]string{"resume"})
	require.NoError(t, err)
	assert.Equal(t, "resume <run-id>", resume.Use)
	require.Error(t, resume.Args(resume, nil))
	require.NoError(t, resume.Args(resume, []string{"run-id"}))
}
