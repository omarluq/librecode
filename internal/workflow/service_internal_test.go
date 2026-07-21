package workflow

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // Register the sqlite database/sql driver used by internal service tests.

	"github.com/omarluq/librecode/internal/database"
)

const serviceStartedEvent = "started"

type serviceTestController struct{}

func (serviceTestController) Submit(context.Context, *AgentRequest) (*database.AgentTaskEntity, error) {
	return nil, errors.New("unexpected agent submission")
}

func (serviceTestController) Get(context.Context, string) (*database.AgentTaskEntity, bool, error) {
	return nil, false, nil
}

func (serviceTestController) Await(context.Context, string) (*database.AgentTaskEntity, error) {
	return nil, errors.New("unexpected agent await")
}

func (serviceTestController) Cancel(context.Context, string, string) (*database.TaskEntity, bool, error) {
	return nil, false, nil
}

func TestServiceDefaultsAndHelperBoundaries(t *testing.T) {
	t.Parallel()

	service, _, owner, _ := newInternalWorkflowService(t)
	run, err := service.Submit(t.Context(), &ServiceRequest{
		Name: "defaults", Source: "1", SourceVersion: "", ArgumentsJSON: "", OwnerSessionID: owner,
	})
	require.NoError(t, err)
	assert.Equal(t, defaultSourceVersion, run.SourceVersion)
	assert.Equal(t, "{}", run.ArgumentsJSON)

	assert.False(t, terminal(database.TaskState("unknown")))
	assert.Empty(t, errorString(nil))
}

func TestServiceRenewLeaseStateBoundaries(t *testing.T) {
	t.Parallel()

	service, repository, owner, _ := newInternalWorkflowService(t)

	queued, err := service.Submit(t.Context(), serviceTestRequest("queued", owner))
	require.NoError(t, err)
	err = service.renewLease(t.Context(), queued.Task.ID)
	require.ErrorContains(t, err, "lease was lost")

	running, err := service.Submit(t.Context(), serviceTestRequest("running", owner))
	require.NoError(t, err)
	claimed, err := repository.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: running.Task.ID, LeaseOwner: service.leaseOwner, EventKind: serviceStartedEvent,
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, service.renewLease(t.Context(), running.Task.ID))

	changed, err := repository.Tasks().Transition(t.Context(), running.Task.ID,
		[]database.TaskState{database.TaskRunning}, database.TaskCanceling, "canceling")
	require.NoError(t, err)
	require.True(t, changed)
	err = service.renewLease(t.Context(), running.Task.ID)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestServiceRejectsMissingAndMalformedPersistedRuns(t *testing.T) {
	t.Parallel()

	service, _, _, _ := newInternalWorkflowService(t)

	_, err := service.ExecuteQueued(t.Context(), "missing")
	require.ErrorContains(t, err, "was not found")
	_, _, err = service.Resume(t.Context(), "missing")
	require.ErrorContains(t, err, "was not found")

	for _, test := range []struct {
		name   string
		resume bool
	}{
		{name: "execute queued", resume: false},
		{name: "resume interrupted", resume: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, repository, owner, connection := newInternalWorkflowService(t)
			run, createErr := service.Submit(t.Context(), serviceTestRequest(test.name, owner))
			require.NoError(t, createErr)
			_, updateErr := connection.ExecContext(t.Context(),
				`UPDATE workflow_runs SET arguments_json = ? WHERE task_id = ?`, `{"broken":`, run.Task.ID)
			require.NoError(t, updateErr)

			if test.resume {
				changed, transitionErr := repository.Tasks().Transition(t.Context(), run.Task.ID,
					[]database.TaskState{database.TaskQueued}, database.TaskInterrupted, "interrupted")
				require.NoError(t, transitionErr)
				require.True(t, changed)
				_, _, createErr = service.Resume(t.Context(), run.Task.ID)
			} else {
				_, createErr = service.ExecuteQueued(t.Context(), run.Task.ID)
			}

			require.ErrorContains(t, createErr, "decode workflow arguments")
		})
	}
}

func TestServiceRenewLeaseDetectsDeletedTask(t *testing.T) {
	t.Parallel()

	service, repository, owner, connection := newInternalWorkflowService(t)
	run, err := service.Submit(t.Context(), serviceTestRequest("leased", owner))
	require.NoError(t, err)
	claimed, err := repository.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: run.Task.ID, LeaseOwner: service.leaseOwner, EventKind: serviceStartedEvent,
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = connection.ExecContext(t.Context(), `
		CREATE TRIGGER delete_renewed_task AFTER UPDATE OF lease_expires_at ON tasks
		BEGIN DELETE FROM tasks WHERE id = NEW.id; END`)
	require.NoError(t, err)

	err = service.renewLease(t.Context(), run.Task.ID)
	require.ErrorContains(t, err, "lease task was not found")
}

func TestServiceFinishEncodingAndStateConflicts(t *testing.T) {
	t.Parallel()

	service, repository, owner, _ := newInternalWorkflowService(t)
	queued, err := service.Submit(t.Context(), serviceTestRequest("queued", owner))
	require.NoError(t, err)
	err = service.finish(t.Context(), queued.Task.ID, serviceTestResult(nil), nil)
	require.ErrorContains(t, err, "was not running")

	running, err := service.Submit(t.Context(), serviceTestRequest("encoding", owner))
	require.NoError(t, err)
	claimed, err := repository.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: running.Task.ID, LeaseOwner: service.leaseOwner, EventKind: serviceStartedEvent,
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, claimed)

	err = service.finish(t.Context(), running.Task.ID, serviceTestResult(make(chan int)), nil)
	require.ErrorContains(t, err, "encode workflow result")
	stored, found, getErr := repository.Get(t.Context(), running.Task.ID)
	require.NoError(t, getErr)
	require.True(t, found)
	assert.Equal(t, database.TaskFailed, stored.Task.State)
}

func TestServiceRepositoryFailurePaths(t *testing.T) {
	t.Parallel()

	service, _, owner, connection := newInternalWorkflowService(t)
	run, err := service.Submit(t.Context(), serviceTestRequest("closed", owner))
	require.NoError(t, err)
	require.NoError(t, connection.Close())

	_, _, err = service.Get(t.Context(), run.Task.ID)
	require.ErrorContains(t, err, "get workflow run")
	_, err = service.List(t.Context(), owner, 10)
	require.ErrorContains(t, err, "list workflow runs")
	_, err = service.Events(t.Context(), run.Task.ID, 0, 10)
	require.ErrorContains(t, err, "list workflow events")
	_, _, err = service.AgentTask(t.Context(), run.Task.ID)
	require.ErrorContains(t, err, "get workflow agent task")
	_, err = service.AgentTasks(t.Context(), run.Task.ID)
	require.ErrorContains(t, err, "list workflow agent tasks")
	_, err = service.AgentTaskDetails(t.Context(), []string{run.Task.ID})
	require.ErrorContains(t, err, "list workflow agent task details")
	_, err = service.RecoverInterrupted(t.Context())
	require.ErrorContains(t, err, "recover interrupted workflow runs")
	_, err = service.Cancel(t.Context(), owner, run.Task.ID)
	require.ErrorContains(t, err, "get workflow run")
	_, err = service.loadVerifiedRun(t.Context(), run.Task.ID)
	require.ErrorContains(t, err, "load persisted workflow run")
	err = service.eventSink(run.Task.ID)(t.Context(), Event{
		Task: TaskResult{ID: "", State: "", Result: "", ErrorCode: "", ErrorMessage: ""},
		Kind: EventTaskLaunched, TaskID: "", NodeKey: "", InvocationIndex: 0,
	})
	require.ErrorContains(t, err, "persist workflow event")
	err = service.renewLease(t.Context(), run.Task.ID)
	require.ErrorContains(t, err, "renew workflow lease")
}

func serviceTestRequest(name, owner string) *ServiceRequest {
	return &ServiceRequest{
		Name: name, Source: "1", SourceVersion: "", ArgumentsJSON: "{}", OwnerSessionID: owner,
	}
}

func serviceTestResult(value any) *RunResult {
	return &RunResult{
		Value: value, Stdout: "", Stderr: "", LaunchedTaskIDs: nil, TaskResults: nil,
	}
}

func newInternalWorkflowService(
	t *testing.T,
) (*Service, *database.WorkflowRepository, string, *sql.DB) {
	t.Helper()

	connection, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, database.Migrate(t.Context(), connection))

	session, err := database.NewSessionRepository(connection).CreateSession(
		t.Context(), t.TempDir(), "workflow owner", "",
	)
	require.NoError(t, err)
	runner, err := NewRunner(serviceTestController{})
	require.NoError(t, err)

	repository := database.NewWorkflowRepository(connection)
	service, err := NewService(repository, runner)
	require.NoError(t, err)

	return service, repository, session.ID, connection
}
