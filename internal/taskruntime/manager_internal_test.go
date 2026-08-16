package taskruntime

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // Register the SQLite driver used by sql.Open.

	"github.com/omarluq/librecode/internal/database"
)

func TestManagerPreservesOwnerScopeAndSpecializedCancellation(t *testing.T) {
	t.Parallel()

	connection := newRuntimeTestDatabase(t, "manager.db")
	sessions, err := database.NewSessionRepository(connection)
	require.NoError(t, err)
	firstOwner, err := sessions.CreateSession(t.Context(), t.TempDir(), "first", "")
	require.NoError(t, err)
	secondOwner, err := sessions.CreateSession(t.Context(), t.TempDir(), "second", "")
	require.NoError(t, err)
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)
	created, err := tasks.Create(t.Context(), &database.TaskEntity{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
		ID: "", Kind: database.TaskKindAgent, OwnerSessionID: firstOwner.ID, ParentTaskID: "",
		ConcurrencyKey: "", LeaseOwner: "", State: "",
		Result: "", ErrorCode: "", ErrorMessage: "",
	})
	require.NoError(t, err)

	manager := NewManager(tasks)
	called := false

	require.NoError(t, manager.RegisterCancel(database.TaskKindAgent, func(
		ctx context.Context, _, taskID string,
	) (*database.TaskEntity, bool, error) {
		called = true
		changed, transitionErr := tasks.Transition(
			ctx, taskID, []database.TaskState{database.TaskQueued}, database.TaskCanceled, "task_canceled", "{}",
		)
		entity, found, getErr := tasks.Get(ctx, taskID)

		return entity, found && changed, contextError(transitionErr, getErr)
	}))

	_, found, err := manager.GetTask(t.Context(), secondOwner.ID, created.ID)
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = manager.CancelTask(t.Context(), secondOwner.ID, created.ID)
	require.NoError(t, err)
	assert.False(t, found)
	assert.False(t, called)

	canceled, found, err := manager.CancelTask(t.Context(), firstOwner.ID, created.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, called)
	assert.Equal(t, database.TaskCanceled, canceled.State)
}

func TestManagerRegistrationAndUnsupportedCancellationErrors(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil)
	cancel := func(context.Context, string, string) (*database.TaskEntity, bool, error) {
		return nil, false, nil
	}

	require.EqualError(t, manager.RegisterCancel("", cancel),
		"taskruntime: task kind and cancellation function are required")
	require.EqualError(t, manager.RegisterCancel(testTaskKind, nil),
		"taskruntime: task kind and cancellation function are required")

	require.NoError(t, manager.RegisterCancel(testTaskKind, cancel))
	require.EqualError(t, manager.RegisterCancel(testTaskKind, cancel),
		"taskruntime: cancellation function already registered")
}

func TestManagerListsOwnedTasksAndReportsLifecycleErrors(t *testing.T) {
	t.Parallel()

	owner, tasks := newRuntimeTestOwnerAndTasks(t, "manager-errors.db")
	created := createRuntimeTestTask(t, tasks, owner.ID, testTaskKind)

	manager := NewManager(tasks)
	listed, err := manager.ListTasks(t.Context(), owner.ID, []database.TaskState{database.TaskQueued}, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, created.ID, listed[0].ID)

	_, found, err := manager.CancelTask(t.Context(), owner.ID, created.ID)
	assert.False(t, found)
	require.EqualError(t, err, "taskruntime: task kind does not support generic cancellation")

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	_, found, err = manager.GetTask(canceled, owner.ID, created.ID)
	assert.False(t, found)
	require.ErrorIs(t, err, context.Canceled)
	_, err = manager.ListTasks(canceled, owner.ID, nil, 0)
	require.ErrorIs(t, err, context.Canceled)
}

func contextError(first, second error) error {
	if first != nil {
		return first
	}

	return second
}

func newRuntimeTestDatabase(tb testing.TB, name string) *sql.DB {
	tb.Helper()

	options := database.SQLiteOptions{BusyTimeout: time.Second}
	connection, err := sql.Open("sqlite", database.SQLiteDSN(filepath.Join(tb.TempDir(), name), options))
	require.NoError(tb, err)
	tb.Cleanup(func() { require.NoError(tb, connection.Close()) })
	connection.SetMaxOpenConns(1)
	require.NoError(tb, database.ConfigureSQLite(tb.Context(), connection, options))
	require.NoError(tb, database.Migrate(tb.Context(), connection))

	return connection
}

func newRuntimeTestOwnerAndTasks(t *testing.T, name string) (*database.SessionEntity, *database.TaskRepository) {
	t.Helper()

	connection := newRuntimeTestDatabase(t, name)
	sessions, err := database.NewSessionRepository(connection)
	require.NoError(t, err)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)

	return owner, tasks
}

func createRuntimeTestTask(
	t *testing.T,
	tasks *database.TaskRepository,
	ownerID string,
	kind string,
) *database.TaskEntity {
	t.Helper()

	created, err := tasks.Create(t.Context(), &database.TaskEntity{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
		ID: "", Kind: kind, ParentTaskID: "", OwnerSessionID: ownerID, ConcurrencyKey: "",
		LeaseOwner: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
	})
	require.NoError(t, err)

	return created
}
