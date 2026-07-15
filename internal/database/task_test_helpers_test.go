package database_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const (
	taskQueuedEvent          = "task_queued"
	taskStartedEvent         = "task_started"
	taskSucceededEvent       = "task_succeeded"
	taskFailedEvent          = "task_failed"
	taskInterruptedEvent     = "task_interrupted"
	invalidID                = "bad"
	eventKindRequired        = "event.kind is required"
	leaseOwnerExpiryRequired = "requires an owner and expiry"
)

type taskTestFixture struct {
	t         *testing.T
	agents    *database.AgentTaskRepository
	tasks     *database.TaskRepository
	workflows *database.WorkflowRepository
	sessions  *database.SessionRepository
}

func newTaskTestFixture(t *testing.T) *taskTestFixture {
	t.Helper()

	connection := openTestSQLite(t, filepath.Join(t.TempDir(), "tasks.db"), 0)
	require.NoError(t, database.Migrate(t.Context(), connection))

	return &taskTestFixture{
		t:         t,
		agents:    database.NewAgentTaskRepository(connection),
		tasks:     database.NewTaskRepository(connection),
		workflows: database.NewWorkflowRepository(connection),
		sessions:  database.NewSessionRepository(connection),
	}
}

func (fixture *taskTestFixture) createOwner(ctx context.Context) *database.SessionEntity {
	fixture.t.Helper()

	owner, err := fixture.sessions.CreateSession(ctx, fixture.t.TempDir(), "owner", "")
	require.NoError(fixture.t, err)

	return owner
}

func (fixture *taskTestFixture) createAgentTaskSessions(
	ctx context.Context,
) (parent, child *database.SessionEntity) {
	fixture.t.Helper()

	parent, err := fixture.sessions.CreateSession(ctx, fixture.t.TempDir(), "parent", "")
	require.NoError(fixture.t, err)
	child, err = fixture.sessions.CreateSession(ctx, parent.CWD, "child", parent.ID)
	require.NoError(fixture.t, err)

	return parent, child
}

func newTask(ownerSessionID string) *database.TaskEntity {
	return &database.TaskEntity{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
		LeaseExpiresAt: nil, ID: "", Kind: database.TaskKindAgent, ParentTaskID: "",
		OwnerSessionID: ownerSessionID, ConcurrencyKey: "", LeaseOwner: "", State: "",
		Result: "", ErrorCode: "", ErrorMessage: "",
	}
}

func newTaskFinish(taskID string, from []database.TaskState, target database.TaskState,
	event string) database.TaskFinish {
	return database.TaskFinish{
		TaskID: taskID, EventKind: event, Result: "", ErrorCode: "", ErrorMessage: "",
		PayloadJSON: `{}`, LeaseOwner: "", TargetState: target, From: from,
	}
}

func newAgentTask(parentSessionID, childSessionID string) *database.AgentTaskEntity {
	return &database.AgentTaskEntity{
		Task: database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
			LeaseExpiresAt: nil, ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: parentSessionID,
			ConcurrencyKey: "", LeaseOwner: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
		},
		ChildSessionID: childSessionID,
		AgentName:      "general",
		Prompt:         "inspect the repository",
		Model:          "",
		Provider:       "",
		PolicyJSON:     "{}",
		UsageJSON:      "{}",
		Depth:          1,
	}
}
