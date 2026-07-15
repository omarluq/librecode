package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

func TestDispatcherExecutesSubmittedWorkflow(t *testing.T) {
	t.Parallel()

	service, repository, owner := newWorkflowService(t, newFakeController())
	dispatcher, err := workflow.NewDispatcher(context.Background(), workflow.DispatcherOptions{
		Service: service, Tasks: repository.Tasks(), Logger: nil,
		Concurrency: 1, Buffer: 4, Interval: 10 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dispatcher.Shutdown(context.Background())) })

	run, err := dispatcher.Submit(t.Context(), &workflow.ServiceRequest{
		Name: "queued", Source: "1 + 1", SourceVersion: "v1", ArgumentsJSON: "{}",
		OwnerSessionID: owner, SourceLimit: 0, OutputLimit: 0,
	})
	require.NoError(t, err)

	completed, err := dispatcher.Await(t.Context(), run.Task.ID)
	require.NoError(t, err)
	assert.Equal(t, database.TaskSucceeded, completed.Task.State)
}

func TestDispatcherRecoversQueuedWorkflow(t *testing.T) {
	t.Parallel()

	service, repository, owner := newWorkflowService(t, newFakeController())
	run, err := service.Submit(t.Context(), &workflow.ServiceRequest{
		Name: "recovered", Source: "2 + 2", SourceVersion: "v1", ArgumentsJSON: "{}",
		OwnerSessionID: owner, SourceLimit: 0, OutputLimit: 0,
	})
	require.NoError(t, err)

	dispatcher, err := workflow.NewDispatcher(context.Background(), workflow.DispatcherOptions{
		Service: service, Tasks: repository.Tasks(), Logger: nil,
		Concurrency: 1, Buffer: 4, Interval: 10 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dispatcher.Shutdown(context.Background())) })

	completed, err := dispatcher.Await(t.Context(), run.Task.ID)
	require.NoError(t, err)
	assert.Equal(t, database.TaskSucceeded, completed.Task.State)
}
