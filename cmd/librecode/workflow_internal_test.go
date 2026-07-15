package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestWorkflowCommandRegistersResume(t *testing.T) {
	t.Parallel()

	resume, _, err := newWorkflowCmd().Find([]string{"resume"})
	require.NoError(t, err)
	assert.Equal(t, "resume <run-id>", resume.Use)
	require.Error(t, resume.Args(resume, nil))
	require.NoError(t, resume.Args(resume, []string{"run-id"}))
}
