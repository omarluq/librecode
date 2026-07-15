package workflow_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

func TestDispatcherClaimsWorkflowSubmittedByAnotherService(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, database.Migrate(t.Context(), connection))

	repository := database.NewWorkflowRepository(connection)
	sessions := database.NewSessionRepository(connection)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "cross-process workflow", "")
	require.NoError(t, err)

	submitterRunner, err := workflow.NewRunner(newFakeController())
	require.NoError(t, err)
	submitter, err := workflow.NewService(repository, submitterRunner)
	require.NoError(t, err)

	workerRunner, err := workflow.NewRunner(newFakeController())
	require.NoError(t, err)
	worker, err := workflow.NewService(database.NewWorkflowRepository(connection), workerRunner)
	require.NoError(t, err)

	run, err := submitter.Submit(t.Context(), &workflow.ServiceRequest{
		Name: "cross-process", Source: "21 * 2", SourceVersion: "v1", ArgumentsJSON: "{}",
		OwnerSessionID: owner.ID, SourceLimit: 0, OutputLimit: 0,
	})
	require.NoError(t, err)

	dispatcher, err := workflow.NewDispatcher(context.Background(), workflow.DispatcherOptions{
		Service:     worker,
		Tasks:       database.NewTaskRepository(connection),
		Logger:      nil,
		Concurrency: 1,
		Buffer:      4,
		Interval:    10 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dispatcher.Shutdown(context.Background())) })

	completed, err := submitter.Await(t.Context(), run.Task.ID)
	require.NoError(t, err)
	assert.Equal(t, database.TaskSucceeded, completed.Task.State)
}
