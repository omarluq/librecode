package workflow_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

const dispatcherQueuedName = "queued"

func TestDispatcherDependencyDefaultsAndShutdownBoundaries(t *testing.T) {
	t.Parallel()

	service, repository, _ := newWorkflowService(t, newFakeController())

	tests := []struct {
		ctx     context.Context
		service *workflow.Service
		tasks   *database.TaskRepository
		name    string
	}{
		{name: "nil context", ctx: nil, service: service, tasks: repository.Tasks()},
		{name: "nil service", ctx: t.Context(), service: nil, tasks: repository.Tasks()},
		{name: "nil tasks", ctx: t.Context(), service: service, tasks: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dispatcher, err := workflow.NewDispatcher(test.ctx, workflow.DispatcherOptions{
				Service:     test.service,
				Tasks:       test.tasks,
				Logger:      nil,
				Concurrency: 0,
				Buffer:      0,
				Interval:    0,
			})
			require.ErrorContains(t, err, "are required")
			assert.Nil(t, dispatcher)
		})
	}

	dispatcher, err := workflow.NewDispatcher(t.Context(), workflow.DispatcherOptions{
		Service:     service,
		Tasks:       repository.Tasks(),
		Logger:      nil,
		Concurrency: 0,
		Buffer:      0,
		Interval:    0,
	})
	require.NoError(t, err)

	_, err = dispatcher.Submit(t.Context(), nil)
	require.ErrorContains(t, err, "request is required")
	require.NoError(t, dispatcher.Shutdown(t.Context()))

	run, err := dispatcher.Submit(t.Context(), &workflow.ServiceRequest{
		Name:           "",
		Source:         "",
		SourceVersion:  "",
		ArgumentsJSON:  "",
		OwnerSessionID: "",
	})
	require.ErrorContains(t, err, "dispatcher is shut down")
	assert.Nil(t, run)
}

func TestDispatcherShutdownDoesNotHoldLifecycleLockAcrossSubmitIO(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(t.Context(), connection))

	repository := database.NewWorkflowRepository(connection)
	sessions := database.NewSessionRepository(connection)
	owner, err := sessions.CreateSession(t.Context(), "/work", "owner", "")
	require.NoError(t, err)
	runner, err := workflow.NewRunner(newFakeController())
	require.NoError(t, err)
	service, err := workflow.NewService(repository, runner)
	require.NoError(t, err)
	dispatcher, err := workflow.NewDispatcher(context.Background(), workflow.DispatcherOptions{
		Service: service, Tasks: repository.Tasks(), Logger: nil, Concurrency: 1, Buffer: 1, Interval: time.Hour,
	})
	require.NoError(t, err)

	transaction, err := connection.BeginTx(t.Context(), nil)
	require.NoError(t, err)

	submitDone := make(chan error, 1)

	go func() {
		_, submitErr := dispatcher.Submit(context.Background(), &workflow.ServiceRequest{
			Name: "blocked", Source: "1", SourceVersion: "v1", ArgumentsJSON: "{}", OwnerSessionID: owner.ID,
		})
		submitDone <- submitErr
	}()

	// Give Submit time to pass the lifecycle check and block waiting for the only DB connection.
	time.Sleep(20 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, dispatcher.Shutdown(shutdownCtx), context.DeadlineExceeded)

	_, err = dispatcher.Submit(t.Context(), &workflow.ServiceRequest{
		Name: "", Source: "", SourceVersion: "", ArgumentsJSON: "", OwnerSessionID: "",
	})
	require.ErrorContains(t, err, "dispatcher is shut down")
	require.NoError(t, transaction.Rollback())
	require.NoError(t, <-submitDone)
	require.NoError(t, dispatcher.Shutdown(context.Background()))
}

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
		Name: dispatcherQueuedName, Source: "1 + 1", SourceVersion: "v1", ArgumentsJSON: "{}",
		OwnerSessionID: owner,
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
		OwnerSessionID: owner,
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
