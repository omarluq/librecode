package workflow_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

const (
	testOwner   = "session-1"
	firstTask   = "task-1"
	secondTask  = "task-2"
	agentSource = `import "librecode/workflow"; id, _ := workflow.Agent("inspect"); workflow.Wait(id)`
)

type fakeController struct {
	tasks    map[string]*database.AgentTaskEntity
	await    func(context.Context, string) (*database.AgentTaskEntity, error)
	submitCh chan string
	taskIDs  []string
	submits  []*workflow.AgentRequest
	cancels  [][2]string
	mu       sync.Mutex
}

func (fake *fakeController) Submit(
	_ context.Context,
	request *workflow.AgentRequest,
) (*database.AgentTaskEntity, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	taskID := fmt.Sprintf("task-%d", len(fake.submits)+1)
	if len(fake.taskIDs) > len(fake.submits) {
		taskID = fake.taskIDs[len(fake.submits)]
	}

	copyRequest := *request
	fake.submits = append(fake.submits, &copyRequest)
	task := agentTask(taskID, request.OwnerSessionID, database.TaskRunning, "")

	fake.tasks[taskID] = task
	if fake.submitCh != nil {
		fake.submitCh <- taskID
	}

	return task, nil
}

func (fake *fakeController) Get(_ context.Context, taskID string) (*database.AgentTaskEntity, bool, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	task, found := fake.tasks[taskID]

	return task, found, nil
}

func (fake *fakeController) Await(ctx context.Context, taskID string) (*database.AgentTaskEntity, error) {
	if fake.await != nil {
		return fake.await(ctx, taskID)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()

	task := fake.tasks[taskID]
	task.Task.State = database.TaskSucceeded
	task.Task.Result = "done"

	return task, nil
}

func (fake *fakeController) Cancel(_ context.Context, owner, taskID string) (*database.TaskEntity, bool, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	fake.cancels = append(fake.cancels, [2]string{owner, taskID})
	task := fake.tasks[taskID]
	task.Task.State = database.TaskCanceled

	return &task.Task, true, nil
}

func TestRunnerSingleTask(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	var events []workflow.Event

	result, err := runner.Run(context.Background(), &workflow.RunRequest{
		Arguments: nil, RunID: "", OnEvent: func(_ context.Context, event workflow.Event) error {
			events = append(events, event)

			return nil
		},
		Name: "", Source: agentSource, OwnerSessionID: testOwner, SourceLimit: 0, OutputLimit: 0,
		PersistedLinks: nil,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{firstTask}, result.LaunchedTaskIDs)
	require.Len(t, fake.submits, 1)
	assert.Equal(t, testOwner, fake.submits[0].OwnerSessionID)
	assert.Equal(t, "inspect", fake.submits[0].Prompt)
	require.Len(t, events, 2)
	assert.Equal(t, workflow.EventTaskLaunched, events[0].Kind)
	assert.Equal(t, workflow.EventTaskCompleted, events[1].Kind)
	assert.Equal(t, "done", events[1].Task.Result)
}

func TestRunnerReusesPersistedInvocationByNormalizedNodeKey(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	fake.tasks[firstTask] = agentTask(firstTask, testOwner, database.TaskSucceeded, "persisted")
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	const source = `import "librecode/workflow"; id, _ := workflow.Agent("inspect"); workflow.Wait(id)`

	result, err := runner.Run(t.Context(), &workflow.RunRequest{
		RunID: "", Name: "", Source: source, OwnerSessionID: testOwner,
		PersistedLinks: []database.WorkflowAgentTaskEntity{{
			CreatedAt: time.Time{}, WorkflowTaskID: "", AgentTaskID: firstTask,
			NodeKey: " agent ", InvocationIndex: 0, Sequence: 1,
		}},
		Arguments: nil, OnEvent: nil, SourceLimit: 0, OutputLimit: 0,
	})
	require.NoError(t, err)
	assert.Empty(t, fake.submits)
	assert.Equal(t, []string{firstTask}, result.LaunchedTaskIDs)
	assert.Equal(t, "done", result.TaskResults[0].Result)
}

func TestRunnerScopesWaitToLaunchedOwnedTasks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup       func(*fakeController)
		source      string
		name        string
		wantCancels [][2]string
	}{
		{
			name:        "unlaunched task",
			source:      `import "librecode/workflow"; workflow.Wait("other")`,
			wantCancels: nil,
			setup: func(fake *fakeController) {
				fake.tasks["other"] = agentTask("other", testOwner, database.TaskSucceeded, "secret")
			},
		},
		{
			name:        "controller owner mismatch",
			source:      agentSource,
			wantCancels: [][2]string{{testOwner, firstTask}},
			setup: func(fake *fakeController) {
				fake.await = func(_ context.Context, taskID string) (*database.AgentTaskEntity, error) {
					return agentTask(taskID, "other-session", database.TaskSucceeded, "secret"), nil
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeController()
			test.setup(fake)
			runner, err := workflow.NewRunner(fake)
			require.NoError(t, err)

			_, err = runner.Run(context.Background(), &workflow.RunRequest{
				Arguments: nil, RunID: "", OnEvent: nil, Name: "", Source: test.source, OwnerSessionID: testOwner,
				SourceLimit: 0, OutputLimit: 0,
				PersistedLinks: nil,
			})
			require.Error(t, err)
			assert.Equal(t, test.wantCancels, fake.cancels)
		})
	}
}

func TestRunnerListAndCancelStayScopedToLaunchOrder(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	const source = `import "librecode/workflow"
first, _ := workflow.Agent("first")
second, _ := workflow.Agent("second")
before, _ := workflow.List()
canceled, _ := workflow.Cancel(second)
[]any{first, second, before, canceled}`

	result, err := runner.Run(context.Background(), &workflow.RunRequest{
		Arguments: nil, RunID: "", OnEvent: nil, Name: "", Source: source, OwnerSessionID: testOwner,
		SourceLimit: 0, OutputLimit: 0,
		PersistedLinks: nil,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{firstTask, secondTask}, result.LaunchedTaskIDs)
	require.Len(t, result.TaskResults, 2)
	assert.Equal(t, firstTask, result.TaskResults[0].ID)
	assert.Equal(t, secondTask, result.TaskResults[1].ID)
	assert.Equal(t, string(database.TaskCanceled), result.TaskResults[1].State)
	assert.Equal(t, [][2]string{{testOwner, secondTask}}, fake.cancels)
}

func TestRunnerPipelinePreservesInputOrder(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	const source = `import "librecode/workflow"
results, _ := workflow.Pipeline([]any{1, 2, 3}, func(item any) (any, error) {
	value := item.(int)
	return value * value, nil
}, 2)
results`

	result, err := runner.Run(context.Background(), &workflow.RunRequest{
		Arguments: nil, RunID: "", OnEvent: nil, Name: "", Source: source, OwnerSessionID: testOwner,
		SourceLimit: 0, OutputLimit: 0,
		PersistedLinks: nil,
	})
	require.NoError(t, err)

	assert.Equal(t, []workflow.PipelineResult{
		{Index: 0, Value: 1, Error: ""},
		{Index: 1, Value: 4, Error: ""},
		{Index: 2, Value: 9, Error: ""},
	}, result.Value)
}

func TestRunnerPipelineHandlesEmptyInput(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	const source = `import "librecode/workflow"
results, _ := workflow.Pipeline([]any{}, func(item any) (any, error) { return item, nil }, 4)
results`

	result, err := runner.Run(context.Background(), &workflow.RunRequest{
		Arguments: nil, RunID: "", OnEvent: nil, Name: "", Source: source, OwnerSessionID: testOwner,
		SourceLimit: 0, OutputLimit: 0,
		PersistedLinks: nil,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Value)
}

func TestRunnerPipelineBoundsConcurrency(t *testing.T) {
	t.Parallel()

	fake := newFakeController()

	var (
		active atomic.Int64
		peak   atomic.Int64
	)

	fake.await = func(_ context.Context, taskID string) (*database.AgentTaskEntity, error) {
		current := active.Add(1)
		updatePeak(&peak, current)

		time.Sleep(10 * time.Millisecond)
		active.Add(-1)

		return agentTask(taskID, testOwner, database.TaskSucceeded, taskID), nil
	}

	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	const source = `import "librecode/workflow"
results, _ := workflow.Pipeline([]any{"a", "b", "c", "d"}, func(item any) (any, error) {
	id, err := workflow.Agent(item.(string))
	if err != nil { return nil, err }
	return workflow.Wait(id)
}, 2)
results`

	result, err := runner.Run(context.Background(), &workflow.RunRequest{
		Arguments: nil, RunID: "", OnEvent: nil, Name: "", Source: source, OwnerSessionID: testOwner,
		SourceLimit: 0, OutputLimit: 0,
		PersistedLinks: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Value, 4)
	assert.LessOrEqual(t, peak.Load(), int64(2))
	assert.Equal(t, int64(2), peak.Load())
}

func TestRunnerPipelineCollectsFailureAndStopsScheduling(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	const source = `import "librecode/workflow"
results, _ := workflow.Pipeline([]any{1, 2, 3}, func(item any) (any, error) {
	value := item.(int)
	if value == 1 { return workflow.Wait("other") }
	return value, nil
}, 1)
results`

	result, err := runner.Run(context.Background(), &workflow.RunRequest{
		Arguments: nil, RunID: "", OnEvent: nil, Name: "", Source: source, OwnerSessionID: testOwner,
		SourceLimit: 0, OutputLimit: 0,
		PersistedLinks: nil,
	})
	require.NoError(t, err)
	assert.Equal(t, []workflow.PipelineResult{
		{
			Index: 0,
			Value: workflow.TaskResult{
				ID: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
			},
			Error: "task was not launched by this workflow",
		},
		{Index: 1, Value: nil, Error: "pipeline stopped before item was scheduled"},
		{Index: 2, Value: nil, Error: "pipeline stopped before item was scheduled"},
	}, result.Value)
}

func TestRunnerPipelineRejectsInvalidConcurrency(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	const source = `import "librecode/workflow"
_, err := workflow.Pipeline([]any{1}, func(item any) (any, error) { return item, nil }, 0)
err`

	_, err = runner.Run(context.Background(), &workflow.RunRequest{
		Arguments: nil, RunID: "", OnEvent: nil, Name: "", Source: source, OwnerSessionID: testOwner,
		SourceLimit: 0, OutputLimit: 0,
		PersistedLinks: nil,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "pipeline concurrency must be positive")
}

func TestRunnerRejectsCancelForUnlaunchedTask(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	fake.tasks["other"] = agentTask("other", testOwner, database.TaskRunning, "")
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	_, err = runner.Run(context.Background(), &workflow.RunRequest{
		Arguments: nil, RunID: "", OnEvent: nil, Name: "",
		Source:         `import "librecode/workflow"; workflow.Cancel("other")`,
		OwnerSessionID: testOwner, SourceLimit: 0, OutputLimit: 0,
		PersistedLinks: nil,
	})
	require.Error(t, err)
	assert.Empty(t, fake.cancels)
}

func TestRunnerEventFailureCancelsActiveLaunchedTasks(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	_, err = runner.Run(context.Background(), &workflow.RunRequest{
		Arguments: nil, RunID: "", OnEvent: func(_ context.Context, _ workflow.Event) error {
			return assert.AnError
		},
		Name: "", Source: `import "librecode/workflow"; workflow.Agent("inspect")`,
		OwnerSessionID: testOwner, SourceLimit: 0, OutputLimit: 0,
		PersistedLinks: nil,
	})
	require.Error(t, err)
	assert.Equal(t, [][2]string{{testOwner, firstTask}}, fake.cancels)
}

func TestRunnerCancellationCancelsActiveLaunchedTasks(t *testing.T) {
	t.Parallel()

	fake := newFakeController()
	fake.submitCh = make(chan string, 1)
	fake.await = func(ctx context.Context, _ string) (*database.AgentTaskEntity, error) {
		<-ctx.Done()

		return nil, ctx.Err()
	}
	runner, err := workflow.NewRunner(fake)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		_, runErr := runner.Run(ctx, &workflow.RunRequest{
			Arguments: nil, RunID: "", OnEvent: nil, Name: "", Source: agentSource, OwnerSessionID: testOwner,
			SourceLimit: 0, OutputLimit: 0,
			PersistedLinks: nil,
		})
		done <- runErr
	}()

	select {
	case <-fake.submitCh:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("workflow did not submit task")
	}

	require.Error(t, <-done)
	assert.Equal(t, [][2]string{{testOwner, firstTask}}, fake.cancels)
}

func updatePeak(peak *atomic.Int64, value int64) {
	for {
		current := peak.Load()
		if value <= current || peak.CompareAndSwap(current, value) {
			return
		}
	}
}

func newFakeController() *fakeController {
	return &fakeController{
		tasks: make(map[string]*database.AgentTaskEntity), await: nil, submitCh: nil, taskIDs: nil,
		submits: nil, cancels: nil, mu: sync.Mutex{},
	}
}

func agentTask(id, owner string, state database.TaskState, result string) *database.AgentTaskEntity {
	return &database.AgentTaskEntity{
		Task: database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
			ID: id, Kind: database.TaskKindAgent, ParentTaskID: "", OwnerSessionID: owner,
			ConcurrencyKey: "", LeaseOwner: "", State: state, Result: result, ErrorCode: "", ErrorMessage: "",
		},
		ChildSessionID: "", AgentName: "", Prompt: "", Model: "", Provider: "", PolicyJSON: "",
		UsageJSON: "", Depth: 0,
	}
}
