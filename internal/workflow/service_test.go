package workflow_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/agenttask"
	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

func TestServicePersistsSuccessfulRun(t *testing.T) {
	t.Parallel()

	service, repository, owner := newWorkflowService(t, newFakeController())
	run, result, err := service.Run(t.Context(), &workflow.ServiceRequest{
		Name: "inspect", Source: "1 + 1", SourceVersion: "v1", ArgumentsJSON: "{}",
		OwnerSessionID: owner, SourceLimit: 0, OutputLimit: 0,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, database.TaskSucceeded, run.Task.State)
	assert.Empty(t, result.LaunchedTaskIDs)

	events, err := service.Events(t.Context(), run.Task.ID, 0, 20)
	require.NoError(t, err)
	assert.Len(t, events, 3)

	stored, found, err := repository.Get(t.Context(), run.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskSucceeded, stored.Task.State)
	assert.NotEmpty(t, stored.Task.Result)
}

func TestServicePersistsFailedRun(t *testing.T) {
	t.Parallel()

	service, _, owner := newWorkflowService(t, newFakeController())
	run, _, err := service.Run(t.Context(), &workflow.ServiceRequest{
		Name: "invalid", Source: `func {`, SourceVersion: "v1", ArgumentsJSON: "{}",
		OwnerSessionID: owner, SourceLimit: 0, OutputLimit: 0,
	})
	require.Error(t, err)

	stored, found, getErr := service.Get(t.Context(), run.Task.ID)
	require.NoError(t, getErr)
	require.True(t, found)
	assert.Equal(t, database.TaskFailed, stored.Task.State)
	assert.Equal(t, "workflow_failed", stored.Task.ErrorCode)
}

func TestServiceExecuteQueuedPersistsEvaluationFailureWithoutReturningIt(t *testing.T) {
	t.Parallel()

	service, _, owner := newWorkflowService(t, newFakeController())
	run, err := service.Submit(t.Context(), &workflow.ServiceRequest{
		Name: "invalid", Source: `func {`, SourceVersion: "v1", ArgumentsJSON: "{}",
		OwnerSessionID: owner, SourceLimit: 0, OutputLimit: 0,
	})
	require.NoError(t, err)

	claimed, err := service.ExecuteQueued(t.Context(), run.Task.ID)
	require.NoError(t, err)
	assert.True(t, claimed)

	stored, found, err := service.Get(t.Context(), run.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskFailed, stored.Task.State)
	assert.Equal(t, "workflow_failed", stored.Task.ErrorCode)
	assert.Contains(t, stored.Task.ErrorMessage, "evaluate workflow source")
}

func TestServiceResumesInterruptedRun(t *testing.T) {
	t.Parallel()

	service, repository, owner := newWorkflowService(t, newFakeController())
	run, err := repository.Create(t.Context(), verifiedWorkflowRunEntity(owner, "1 + 1"))
	require.NoError(t, err)
	changed, err := repository.Tasks().Transition(t.Context(), run.Task.ID,
		[]database.TaskState{database.TaskQueued}, database.TaskInterrupted, "workflow_interrupted")
	require.NoError(t, err)
	require.True(t, changed)

	resumed, result, err := service.Resume(t.Context(), run.Task.ID)
	require.NoError(t, err)
	assert.Equal(t, database.TaskSucceeded, resumed.Task.State)
	assert.Equal(t, 2, result.Value)

	events, err := service.Events(t.Context(), run.Task.ID, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, "workflow_resumed", events[2].Event.Kind)
}

func TestServiceResumeVerifiesPersistedSourceHash(t *testing.T) {
	t.Parallel()

	service, repository, owner := newWorkflowService(t, newFakeController())
	run, err := repository.Create(t.Context(), workflowRunEntity(owner))
	require.NoError(t, err)

	_, _, err = service.Resume(t.Context(), run.Task.ID)
	require.ErrorContains(t, err, "source hash differs")
}

func TestServiceRejectsCancelFromAnotherOwner(t *testing.T) {
	t.Parallel()

	service, repository, owner := newWorkflowService(t, newFakeController())
	run, err := repository.Create(t.Context(), workflowRunEntity(owner))
	require.NoError(t, err)

	canceled, err := service.Cancel(t.Context(), "other-owner", run.Task.ID)
	require.NoError(t, err)
	assert.False(t, canceled)
}

func TestServiceIntegratesDurableAgentTaskLifecycle(t *testing.T) {
	t.Parallel()

	environment := newWorkflowIntegration(t)
	outcome := make(chan workflowRunOutcome, 1)

	go func() {
		run, result, err := environment.workflows.Run(context.Background(), &workflow.ServiceRequest{
			Name: "durable-agent", Source: agentSource, SourceVersion: "", ArgumentsJSON: "{}",
			OwnerSessionID: environment.owner, SourceLimit: 0, OutputLimit: 0,
		})
		outcome <- workflowRunOutcome{run: run, result: result, err: err}
	}()

	taskID := awaitIntegrationTask(t, environment.runner.started)
	runs, err := environment.workflows.List(t.Context(), environment.owner, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	runID := runs[0].Task.ID

	task, found, err := environment.agentTasks.Get(t.Context(), taskID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskRunning, task.Task.State)
	assert.Equal(t, runID, task.Task.ParentTaskID)

	require.Eventually(t, func() bool {
		links, linkErr := environment.workflows.AgentTasks(t.Context(), runID)

		return linkErr == nil && len(links) == 1 && links[0].AgentTaskID == taskID
	}, time.Second, 10*time.Millisecond)

	environment.runner.unblock()

	completed := <-outcome
	require.NoError(t, completed.err)
	require.NotNil(t, completed.run)
	require.NotNil(t, completed.result)
	assert.Equal(t, database.TaskSucceeded, completed.run.Task.State)
	assert.Equal(t, []string{taskID}, completed.result.LaunchedTaskIDs)
	require.Len(t, completed.result.TaskResults, 1)
	assert.Equal(t, "integrated result", completed.result.TaskResults[0].Result)

	persisted, found, err := environment.agentTasks.Get(t.Context(), taskID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskSucceeded, persisted.Task.State)
	assert.Equal(t, "integrated result", persisted.Task.Result)
}

func TestServiceCancellationCascadesToDurableAgentTask(t *testing.T) {
	t.Parallel()

	environment := newWorkflowIntegration(t)
	outcome := make(chan workflowRunOutcome, 1)

	go func() {
		run, result, err := environment.workflows.Run(context.Background(), &workflow.ServiceRequest{
			Name: "cancel-agent", Source: agentSource, SourceVersion: "", ArgumentsJSON: "{}",
			OwnerSessionID: environment.owner, SourceLimit: 0, OutputLimit: 0,
		})
		outcome <- workflowRunOutcome{run: run, result: result, err: err}
	}()

	taskID := awaitIntegrationTask(t, environment.runner.started)
	runs, err := environment.workflows.List(t.Context(), environment.owner, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	canceled, err := environment.workflows.Cancel(t.Context(), environment.owner, runs[0].Task.ID)
	require.NoError(t, err)
	require.True(t, canceled)

	completed := <-outcome
	require.Error(t, completed.err)
	require.NotNil(t, completed.run)
	assert.Equal(t, database.TaskCanceled, completed.run.Task.State)

	agentResult, err := environment.tasks.Await(t.Context(), taskID)
	require.NoError(t, err)
	assert.Equal(t, database.TaskCanceled, agentResult.Task.State)
}

func newWorkflowService(
	t *testing.T,
	controller workflow.Controller,
) (*workflow.Service, *database.WorkflowRepository, string) {
	t.Helper()

	connection, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, database.Migrate(t.Context(), connection))

	repository := database.NewWorkflowRepository(connection)
	sessions := database.NewSessionRepository(connection)
	owner, err := sessions.CreateSession(t.Context(), "/work", "workflow owner", "")
	require.NoError(t, err)

	runner, err := workflow.NewRunner(controller)
	require.NoError(t, err)
	service, err := workflow.NewService(repository, runner)
	require.NoError(t, err)

	return service, repository, owner.ID
}

func verifiedWorkflowRunEntity(owner, source string) *database.WorkflowRunEntity {
	run := workflowRunEntity(owner)
	hash := sha256.Sum256([]byte(source))
	run.Source = source
	run.SourceHash = hex.EncodeToString(hash[:])

	return run
}

func workflowRunEntity(owner string) *database.WorkflowRunEntity {
	return &database.WorkflowRunEntity{
		Task: database.TaskEntity{
			CreatedAt: databaseZeroTime(), StartedAt: nil, FinishedAt: nil, UpdatedAt: databaseZeroTime(),
			LeaseExpiresAt: nil, ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: owner,
			ConcurrencyKey: owner, LeaseOwner: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
		},
		Name: "test workflow", Source: "1", SourceHash: "hash", SourceVersion: "v1", ArgumentsJSON: "{}",
	}
}

func databaseZeroTime() (zeroTime time.Time) {
	return zeroTime
}

type integrationAgentRunner struct {
	started chan string
	release chan struct{}
	once    sync.Once
}

func (runner *integrationAgentRunner) Run(
	ctx context.Context,
	task *database.AgentTaskEntity,
	_ agenttask.EventSink,
) (agenttask.Result, error) {
	runner.started <- task.Task.ID

	select {
	case <-ctx.Done():
		return agenttask.Result{}, errors.Join(errors.New("integration agent canceled"), ctx.Err())
	case <-runner.release:
		return agenttask.Result{Text: "integrated result", UsageJSON: `{}`}, nil
	}
}

func (runner *integrationAgentRunner) unblock() {
	runner.once.Do(func() { close(runner.release) })
}

type workflowIntegration struct {
	workflows  *workflow.Service
	tasks      *agenttask.Service
	agentTasks *database.AgentTaskRepository
	runner     *integrationAgentRunner
	owner      string
}

type workflowRunOutcome struct {
	run    *database.WorkflowRunEntity
	result *workflow.RunResult
	err    error
}

func newWorkflowIntegration(t *testing.T) *workflowIntegration {
	t.Helper()

	connection, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, database.Migrate(t.Context(), connection))

	taskRepository := database.NewTaskRepository(connection)
	agentTaskRepository := database.NewAgentTaskRepository(connection)
	sessions := database.NewSessionRepository(connection)
	workflowRepository := database.NewWorkflowRepository(connection)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "workflow integration owner", "")
	require.NoError(t, err)

	agentRunner := &integrationAgentRunner{
		started: make(chan string, 1), release: make(chan struct{}), once: sync.Once{},
	}
	taskService, err := agenttask.New(context.Background(), &agenttask.Options{
		Tasks: taskRepository, AgentTasks: agentTaskRepository, Workflows: workflowRepository,
		Runner: agentRunner, Logger: nil,
		Concurrency: 1, SessionConcurrency: 1, QueueCapacity: 4, Timeout: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		agentRunner.unblock()
		require.NoError(t, taskService.Shutdown(context.Background()))
	})

	submitter, err := assistant.NewAgentSubmitter(taskService, agent.Load(t.TempDir()))
	require.NoError(t, err)
	controller, err := assistant.NewWorkflowController(submitter, taskService, sessions)
	require.NoError(t, err)
	workflowRunner, err := workflow.NewRunner(controller)
	require.NoError(t, err)
	workflowService, err := workflow.NewService(workflowRepository, workflowRunner)
	require.NoError(t, err)

	return &workflowIntegration{
		workflows: workflowService, tasks: taskService, agentTasks: agentTaskRepository,
		runner: agentRunner, owner: owner.ID,
	}
}

func awaitIntegrationTask(t *testing.T, started <-chan string) string {
	t.Helper()

	select {
	case taskID := <-started:
		return taskID
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for integrated agent task to start")

		return ""
	}
}
