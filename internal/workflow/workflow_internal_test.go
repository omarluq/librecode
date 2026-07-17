package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/executeworker"
)

type branchController struct {
	submit func(context.Context, *AgentRequest) (*database.AgentTaskEntity, error)
	get    func(context.Context, string) (*database.AgentTaskEntity, bool, error)
	await  func(context.Context, string) (*database.AgentTaskEntity, error)
	cancel func(context.Context, string, string) (*database.TaskEntity, bool, error)
}

func (controller *branchController) Submit(
	ctx context.Context,
	request *AgentRequest,
) (*database.AgentTaskEntity, error) {
	return controller.submit(ctx, request)
}

func (controller *branchController) Get(ctx context.Context, id string) (*database.AgentTaskEntity, bool, error) {
	return controller.get(ctx, id)
}

func (controller *branchController) Await(ctx context.Context, id string) (*database.AgentTaskEntity, error) {
	return controller.await(ctx, id)
}

func (controller *branchController) Cancel(ctx context.Context, owner, id string) (*database.TaskEntity, bool, error) {
	return controller.cancel(ctx, owner, id)
}

func TestRunnerRejectsInvalidConstructionAndRunInputs(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(nil)
	require.Error(t, err)
	assert.Nil(t, runner)

	runner = &Runner{controller: nil, executable: ""}

	var nilContext context.Context

	_, err = runner.Run(nilContext, &RunRequest{
		RunID: "", Name: "", Source: "", OwnerSessionID: "", OnEvent: nil,
		Arguments: nil, PersistedLinks: nil,
	})
	require.ErrorContains(t, err, "context is required")
	_, err = runner.Run(t.Context(), nil)
	require.ErrorContains(t, err, "request is required")
}

func TestWorkflowValueNormalizationFallbacks(t *testing.T) {
	t.Parallel()

	unencodable := make(chan int)
	assert.Equal(t, unencodable, normalizeWorkflowValue(unencodable))
	assert.InDelta(t, 1.25, normalizeNumbers(json.Number("1.25")), 0)
	assert.Equal(t, "not-a-number", normalizeNumbers(json.Number("not-a-number")))

	original := map[string]any{"number": json.Number("2"), "items": []any{json.Number("3")}}
	assert.Equal(t, map[string]any{"number": 2, "items": []any{3}}, normalizeNumbers(original))

	assert.Equal(t, "ordinary", normalizePipelineResults("ordinary"))
	assert.Equal(t, []PipelineResult{}, normalizePipelineResults([]any{}))

	for _, malformed := range []any{
		[]any{"not fields"},
		[]any{map[string]any{"index": 0, "value": "x"}},
		[]any{map[string]any{"index": "zero", "value": "x", "error": ""}},
	} {
		assert.Equal(t, malformed, normalizePipelineResults(malformed))
	}
}

func TestRunHostRPCValidation(t *testing.T) {
	t.Parallel()

	host := newBranchHost(newBranchController())
	message := branchMessage("workflow_agent")
	message.Input = []byte("{")
	_, err := host.handleRPC(t.Context(), message)
	require.ErrorContains(t, err, "decode agent request")
	_, err = host.handleRPC(t.Context(), branchMessage("unknown"))
	require.ErrorContains(t, err, "unknown workflow RPC")
}

func TestRunHostAgentFailureBranches(t *testing.T) {
	t.Parallel()

	failure := errors.New("controller failure")
	tests := []struct {
		name       string
		controller *branchController
		want       string
	}{
		{
			name: "submit failure",
			controller: &branchController{
				submit: func(
					context.Context, *AgentRequest,
				) (*database.AgentTaskEntity, error) {
					return nil, failure
				}, get: nil, await: nil, cancel: nil},
			want: "submit agent task",
		},
		{
			name: "empty task",
			controller: &branchController{
				submit: func(
					context.Context, *AgentRequest,
				) (*database.AgentTaskEntity, error) {
					return branchAgentTask("", testBranchOwner, database.TaskRunning), nil
				}, get: nil, await: nil, cancel: nil},
			want: "empty task",
		},
		{
			name: "wrong owner",
			controller: &branchController{
				submit: func(
					context.Context, *AgentRequest,
				) (*database.AgentTaskEntity, error) {
					return branchAgentTask("task", "other", database.TaskRunning), nil
				}, get: nil, await: nil, cancel: nil},
			want: "another session",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			host := newBranchHost(test.controller)
			_, err := host.agent("prompt")
			require.ErrorContains(t, err, test.want)
		})
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	host := newBranchHost(newBranchController())
	host.ctx = ctx
	_, err := host.agent("prompt")
	require.ErrorContains(t, err, "launch agent")

	_, err = newBranchHost(newBranchController()).agent("")
	require.ErrorContains(t, err, "prompt is required")

	options := AgentOptions{
		NodeKey: "", AgentName: "", Model: "", Provider: "", ConcurrencyKey: "", Depth: 0,
	}
	_, err = newBranchHost(newBranchController()).agent("prompt", options, options)
	require.ErrorContains(t, err, "at most one")
}

func TestRunHostWaitFailureBranches(t *testing.T) {
	t.Parallel()

	failure := errors.New("controller failure")
	tests := []struct {
		name       string
		controller *branchController
		want       string
	}{
		{
			name: "get failure",
			controller: &branchController{
				submit: nil,
				get: func(
					context.Context, string,
				) (*database.AgentTaskEntity, bool, error) {
					return nil, false, failure
				}, await: nil, cancel: nil},
			want: "get agent task",
		},
		{
			name: "not found",
			controller: &branchController{
				submit: nil,
				get: func(
					context.Context, string,
				) (*database.AgentTaskEntity, bool, error) {
					return nil, false, nil
				}, await: nil, cancel: nil},
			want: taskNotOwnedBySession,
		},
		{
			name: "await failure",
			controller: &branchController{
				submit: nil, get: ownedGet,
				await:  func(context.Context, string) (*database.AgentTaskEntity, error) { return nil, failure },
				cancel: nil,
			},
			want: "await agent task",
		},
		{
			name: "nil awaited task",
			controller: &branchController{
				submit: nil, get: ownedGet,
				await: func(context.Context, string) (*database.AgentTaskEntity, error) {
					return branchAgentTask("task", "other", database.TaskRunning), nil
				},
				cancel: nil,
			},
			want: "another session",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			host := newBranchHost(test.controller)
			host.launched["task"] = struct{}{}
			_, err := host.wait("task")
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestRunHostListAndCancelFailureBranches(t *testing.T) {
	t.Parallel()

	failure := errors.New("controller failure")
	controller := newBranchController()
	controller.get = func(context.Context, string) (*database.AgentTaskEntity, bool, error) {
		return nil, false, failure
	}
	host := newBranchHost(controller)
	host.launched["task"] = struct{}{}
	host.taskIDs = []string{"task"}
	_, err := host.list()
	require.ErrorContains(t, err, "get launched task")

	controller = newBranchController()
	controller.get = ownedGet
	controller.cancel = func(context.Context, string, string) (*database.TaskEntity, bool, error) {
		return nil, false, failure
	}
	host.controller = controller
	_, err = host.cancel("task")
	require.ErrorContains(t, err, "cancel agent task")

	controller = newBranchController()
	controller.get = ownedGet
	controller.cancel = func(context.Context, string, string) (*database.TaskEntity, bool, error) {
		return nil, false, nil
	}
	host.controller = controller
	_, err = host.cancel("task")
	require.ErrorContains(t, err, taskNotOwnedBySession)
}

func TestRunHostPersistedAndCleanupFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("controller failure")
	controller := newBranchController()
	controller.get = func(context.Context, string) (*database.AgentTaskEntity, bool, error) {
		return nil, false, failure
	}
	host := newBranchHost(controller)
	host.persisted[invocationKey{nodeKey: "agent", index: 0}] = persistedInvocation{taskID: "task"}
	_, _, err := host.persistedTask("agent", 0)
	require.ErrorContains(t, err, "get persisted agent task")

	cancelCalls := 0
	host = newBranchHost(&branchController{
		submit: nil,
		get: func(_ context.Context, taskID string) (*database.AgentTaskEntity, bool, error) {
			if taskID == "get-error" {
				return nil, false, failure
			}

			switch taskID {
			case "terminal":
				return branchAgentTask(taskID, testBranchOwner, database.TaskSucceeded), true, nil
			default:
				return branchAgentTask(taskID, testBranchOwner, database.TaskRunning), true, nil
			}
		},
		await: nil,
		cancel: func(context.Context, string, string) (*database.TaskEntity, bool, error) {
			cancelCalls++

			return nil, false, failure
		},
	})
	host.taskIDs = []string{"get-error", "terminal", "running"}
	err = host.cancelActive()
	require.ErrorContains(t, err, "get launched task get-error")
	require.ErrorContains(t, err, "cancel launched task running")
	assert.Equal(t, 1, cancelCalls)
}

func TestTerminalStates(t *testing.T) {
	t.Parallel()

	for _, state := range []database.TaskState{
		database.TaskSucceeded, database.TaskFailed, database.TaskCanceled, database.TaskInterrupted,
	} {
		assert.True(t, terminal(state))
	}

	for _, state := range []database.TaskState{
		database.TaskQueued, database.TaskRunning, database.TaskCanceling, database.TaskState("unknown"),
	} {
		assert.False(t, terminal(state))
	}
}

const testBranchOwner = "branch-owner"

func newBranchController() *branchController {
	return &branchController{submit: nil, get: nil, await: nil, cancel: nil}
}

func branchMessage(method string) *executeworker.Message {
	return &executeworker.Message{
		Stderr: "", Source: "", Method: method, Mode: "", Name: "", Query: "", Stdout: "", Type: "",
		Error: "", ErrorKind: "", ValueKind: "", Input: nil, Value: nil, Arguments: nil,
		ID: 0, ExitCode: 0,
	}
}

func newBranchHost(controller Controller) *runHost {
	return &runHost{
		ctx: context.Background(), controller: controller, runID: "", onEvent: nil,
		ownerSessionID: testBranchOwner, launched: make(map[string]struct{}),
		invocations: make(map[string]int), persisted: make(map[invocationKey]persistedInvocation),
		taskIDs: make([]string, 0), mu: sync.Mutex{}, launchMu: sync.Mutex{}, eventMu: sync.Mutex{},
	}
}

func ownedGet(_ context.Context, taskID string) (*database.AgentTaskEntity, bool, error) {
	return branchAgentTask(taskID, testBranchOwner, database.TaskRunning), true, nil
}

func branchAgentTask(id, owner string, state database.TaskState) *database.AgentTaskEntity {
	return &database.AgentTaskEntity{
		Task: database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
			ID: id, Kind: "", ParentTaskID: "", OwnerSessionID: owner, ConcurrencyKey: "", LeaseOwner: "",
			State: state, Result: "", ErrorCode: "", ErrorMessage: "",
		},
		ChildSessionID: "", AgentName: "", Prompt: "", Model: "", Provider: "", PolicyJSON: "", UsageJSON: "", Depth: 0,
	}
}
