package workflow_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // Register the sqlite database/sql driver used by this cross-process test.

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

func TestDispatcherClaimsWorkflowSubmittedByAnotherService(t *testing.T) {
	t.Parallel()

	databasePath := t.TempDir() + "/workflow.db"
	dsn := database.SQLiteDSN(databasePath, database.SQLiteOptions{BusyTimeout: time.Second})
	submitterDB, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, submitterDB.Close()) })
	require.NoError(t, database.Migrate(t.Context(), submitterDB))

	workerDB, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, workerDB.Close()) })

	repository := database.NewWorkflowRepository(submitterDB)
	sessions := database.NewSessionRepository(submitterDB)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "cross-process workflow", "")
	require.NoError(t, err)

	submitterRunner, err := workflow.NewRunner(newFakeController())
	require.NoError(t, err)
	submitter, err := workflow.NewService(repository, submitterRunner)
	require.NoError(t, err)

	workerRunner, err := workflow.NewRunner(newFakeController())
	require.NoError(t, err)
	worker, err := workflow.NewService(database.NewWorkflowRepository(workerDB), workerRunner)
	require.NoError(t, err)

	run, err := submitter.Submit(t.Context(), &workflow.ServiceRequest{
		Name: "cross-process", Source: "21 * 2", SourceVersion: "v1", ArgumentsJSON: "{}",
		OwnerSessionID: owner.ID,
	})
	require.NoError(t, err)

	dispatcher, err := workflow.NewDispatcher(context.Background(), workflow.DispatcherOptions{
		Service:     worker,
		Tasks:       database.NewTaskRepository(workerDB),
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
