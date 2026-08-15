package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/tooltask"
)

const backgroundTestOwner = "background-owner"

const backgroundTestDone = "done"

type recordingToolLifecycle struct {
	events []string
}

func (lifecycle *recordingToolLifecycle) ExecuteCommand(context.Context, string, string) (string, error) {
	return "", nil
}
func (lifecycle *recordingToolLifecycle) Emit(context.Context, string, map[string]any) error {
	return nil
}
func (lifecycle *recordingToolLifecycle) ExecuteTool(
	context.Context, string, tool.Arguments,
) (extension.ToolResult, error) {
	return extension.ToolResult{Details: nil, Content: ""}, nil
}
func (lifecycle *recordingToolLifecycle) Tools() []extension.Tool { return nil }
func (lifecycle *recordingToolLifecycle) DispatchLifecycle(
	_ context.Context, event extension.LifecycleEvent,
) (extension.LifecycleDispatchResult, error) {
	payload := event.Payload
	lifecycle.events = append(lifecycle.events, fmt.Sprintf(
		"%s:%v:%v", event.Name, payload["call_id"], payload["parent_call_id"],
	))

	return emptyTestLifecycleDispatchResult(event), nil
}

type backgroundTaskController struct {
	startErr     error
	getErr       error
	listErr      error
	cancelErr    error
	startRequest *tooltask.StartRequest
	entity       *database.ToolTaskEntity
	listOwner    string
	cancelOwner  string
	listStates   []database.TaskState
	listLimit    int
	found        bool
}

func newBackgroundTaskController() *backgroundTaskController {
	owner := backgroundTestOwner

	return &backgroundTaskController{
		startRequest: nil,
		startErr:     nil,
		getErr:       nil,
		listErr:      nil,
		cancelErr:    nil,
		entity: &database.ToolTaskEntity{
			OutcomeVersion: nil, OutcomeJSON: nil,
			Task: database.TaskEntity{
				CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
				ID: uuid.Must(uuid.NewV7()).String(), Kind: database.TaskKindTool, ParentTaskID: "",
				OwnerSessionID: owner, ConcurrencyKey: "", LeaseOwner: "", State: database.TaskQueued,
				Result: "", ErrorCode: "", ErrorMessage: "",
			},
			WrapperCallID: "", OwnerSessionID: owner, InvocationID: "", CWD: "", ParentCallID: "",
			InitiatingEntryID: "", PolicyJSON: "", DefinitionJSON: "", ArgumentsJSON: "",
			TargetName: string(tool.NameRead), SourceSequence: 0, TimeoutSeconds: 0,
		},
		listOwner:   "",
		listStates:  nil,
		listLimit:   0,
		cancelOwner: "",
		found:       true,
	}
}

func (controller *backgroundTaskController) Start(
	_ context.Context,
	request *tooltask.StartRequest,
) (*database.ToolTaskEntity, error) {
	copied := *request
	controller.startRequest = &copied

	return controller.entity, controller.startErr
}

func (controller *backgroundTaskController) Get(
	_ context.Context,
	owner, taskID string,
) (*database.ToolTaskEntity, bool, error) {
	if controller.getErr != nil {
		return nil, false, controller.getErr
	}

	if !controller.found || owner != controller.entity.OwnerSessionID || taskID != controller.entity.Task.ID {
		return nil, false, nil
	}

	return controller.entity, true, nil
}

func (controller *backgroundTaskController) List(
	_ context.Context,
	owner string,
	states []database.TaskState,
	limit int,
) ([]database.ToolTaskEntity, error) {
	controller.listOwner = owner
	controller.listStates = states

	controller.listLimit = limit
	if controller.listErr != nil {
		return nil, controller.listErr
	}

	return []database.ToolTaskEntity{*controller.entity}, nil
}

func (controller *backgroundTaskController) Cancel(
	_ context.Context,
	owner, taskID string,
) (*database.ToolTaskEntity, bool, error) {
	controller.cancelOwner = owner
	if controller.cancelErr != nil {
		return nil, false, controller.cancelErr
	}

	if !controller.found || owner != controller.entity.OwnerSessionID || taskID != controller.entity.Task.ID {
		return nil, false, nil
	}

	controller.entity.Task.State = database.TaskCanceled

	return controller.entity, true, nil
}

func (controller *backgroundTaskController) Wait(
	context.Context,
	string,
	string,
) (*database.ToolTaskEntity, error) {
	return controller.entity, nil
}

func TestRuntimeToolTaskWrappers(t *testing.T) {
	t.Parallel()

	states := []database.TaskState{database.TaskQueued, database.TaskRunning}
	controller := newBackgroundTaskController()
	runtime := newDetachTestRuntime(controller)

	tasks, err := runtime.ToolTasks(t.Context(), backgroundTestOwner, states, 17)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, controller.entity.Task.ID, tasks[0].Task.ID)
	assert.Equal(t, backgroundTestOwner, controller.listOwner)
	assert.Equal(t, states, controller.listStates)
	assert.Equal(t, 17, controller.listLimit)

	entity, found, err := runtime.ToolTask(t.Context(), backgroundTestOwner, controller.entity.Task.ID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Same(t, controller.entity, entity)

	entity, found, err = runtime.CancelToolTask(t.Context(), backgroundTestOwner, controller.entity.Task.ID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Same(t, controller.entity, entity)
	assert.Equal(t, backgroundTestOwner, controller.cancelOwner)
}

func TestRuntimeToolTaskWrappersWithoutController(t *testing.T) {
	t.Parallel()

	var nilRuntime *Runtime
	for _, testCase := range []struct {
		runtime *Runtime
		name    string
	}{
		{name: "nil runtime", runtime: nilRuntime},
		{name: "runtime without controller", runtime: new(Runtime)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tasks, err := testCase.runtime.ToolTasks(t.Context(), "owner", nil, 1)
			requireRuntimeOopsCode(t, err, "tool_task_service_unavailable")
			assert.Nil(t, tasks)

			entity, found, err := testCase.runtime.ToolTask(t.Context(), "owner", "task")
			requireRuntimeOopsCode(t, err, "tool_task_service_unavailable")
			assert.Nil(t, entity)
			assert.False(t, found)

			entity, found, err = testCase.runtime.CancelToolTask(t.Context(), "owner", "task")
			requireRuntimeOopsCode(t, err, "tool_task_service_unavailable")
			assert.Nil(t, entity)
			assert.False(t, found)
		})
	}
}

func TestRuntimeToolTaskWrappersWrapControllerErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		configure func(*backgroundTaskController)
		call      func(*testing.T, *Runtime, *backgroundTaskController) error
		name      string
		code      string
	}{
		{
			name: "list", code: "list_tool_tasks",
			configure: func(controller *backgroundTaskController) { controller.listErr = errors.New("list failed") },
			call: func(t *testing.T, runtime *Runtime, _ *backgroundTaskController) error {
				t.Helper()
				_, err := runtime.ToolTasks(t.Context(), "owner", nil, 1)

				return err
			},
		},
		{
			name: "get", code: "get_tool_task",
			configure: func(controller *backgroundTaskController) { controller.getErr = errors.New("get failed") },
			call: func(t *testing.T, runtime *Runtime, controller *backgroundTaskController) error {
				t.Helper()
				_, _, err := runtime.ToolTask(t.Context(), "owner", controller.entity.Task.ID)

				return err
			},
		},
		{
			name: "cancel", code: "cancel_tool_task",
			configure: func(controller *backgroundTaskController) {
				controller.cancelErr = errors.New("cancel failed")
			},
			call: func(t *testing.T, runtime *Runtime, controller *backgroundTaskController) error {
				t.Helper()
				_, _, err := runtime.CancelToolTask(t.Context(), "owner", controller.entity.Task.ID)

				return err
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			controller := newBackgroundTaskController()
			testCase.configure(controller)
			runtime := newDetachTestRuntime(controller)

			requireRuntimeOopsCode(t, testCase.call(t, runtime, controller), testCase.code)
		})
	}
}

func TestBackgroundToolDefinitionKeepsOrdinarySchemaFirst(t *testing.T) {
	t.Parallel()

	executor := backgroundTestExecutor(t, newBackgroundTaskController(), tool.NameRead, backgroundTestOwner)
	definition := executor.Definition()

	var schema map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(definition.Schema.RawMessage(), &schema))
	assert.JSONEq(t, `"object"`, string(schema[jsonTypeKey]))

	var variants []json.RawMessage
	require.NoError(t, json.Unmarshal(schema["oneOf"], &variants))
	require.Len(t, variants, 3)
	assert.JSONEq(t, string(executor.target.Definition().Schema.RawMessage()), string(variants[0]))
}

func TestBackgroundToolDefinitionIsConcurrentSafe(t *testing.T) {
	t.Parallel()

	executor := backgroundTestExecutor(t, newBackgroundTaskController(), tool.NameRead, backgroundTestOwner)

	const callers = 16

	definitions := make(chan tool.Definition, callers)

	var workers sync.WaitGroup

	workers.Add(callers)

	for range callers {
		go func() {
			defer workers.Done()

			definitions <- executor.Definition()
		}()
	}

	workers.Wait()
	close(definitions)

	want := string(executor.Definition().Schema.RawMessage())
	for definition := range definitions {
		assert.JSONEq(t, want, string(definition.Schema.RawMessage()))
	}
}

func TestPromptRegistryReplacesTaskToolsWithEligibleBackgroundEnvelopes(t *testing.T) {
	t.Parallel()

	controller := newBackgroundTaskController()
	runtime := newDetachTestRuntime(controller)
	runtime.profile = topLevelExecutionProfile()
	registry, err := runtime.promptToolRegistry(t.Context(), t.TempDir(), backgroundTestOwner)
	require.NoError(t, err)

	for _, definition := range registry.Definitions() {
		assert.False(t, strings.HasPrefix(string(definition.Name), "task_"))

		var schema map[string]any
		require.NoError(t, json.Unmarshal(definition.Schema.RawMessage(), &schema))
		_, augmented := schema["oneOf"]
		assert.Equal(t, tooltask.Eligible(definition.Name), augmented, definition.Name)
	}
}

func TestForegroundOutcomeContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		outcome   string
		waitErr   error
		wantError string
		wantText  string
	}{
		{
			name: "wait error", outcome: "", waitErr: errors.New("wait failed"),
			wantError: "wait failed", wantText: "",
		},
		{
			name: "missing outcome", outcome: "", waitErr: nil,
			wantError: "without an outcome", wantText: "",
		},
		{
			name: "invalid outcome", outcome: "not-json", waitErr: nil,
			wantError: "decode foreground outcome", wantText: "",
		},
		{
			name: "tool failure", outcome: `{"result":{"content":[]},"error":"tool failed","is_error":true}`,
			waitErr: nil, wantError: "tool failed", wantText: "",
		},
		{
			name: "success", outcome: `{"result":{"content":[{"type":"text","text":"done"}]},"is_error":false}`,
			waitErr: nil, wantError: "", wantText: backgroundTestDone,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var entity *database.ToolTaskEntity
			if testCase.outcome != "" {
				entity = new(database.ToolTaskEntity)
				entity.OutcomeJSON = &testCase.outcome
			}

			result, err := foregroundOutcome(entity, testCase.waitErr)
			if testCase.wantError != "" {
				require.ErrorContains(t, err, testCase.wantError)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantText, result.Text())
		})
	}
}

func TestBackgroundToolForegroundCallRemainsUnchanged(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	controller := newBackgroundTaskController()
	runtime := newDetachTestRuntime(controller)
	runtime.profile = topLevelExecutionProfile()
	registry, err := runtime.promptToolRegistry(t.Context(), directory, backgroundTestOwner)
	require.NoError(t, err)

	_, err = registry.Execute(t.Context(), string(tool.NameRead), mustArguments(t, `{"path":"missing"}`))
	require.Error(t, err)
	assert.Nil(t, controller.startRequest)
}

func TestBackgroundToolStartReturnsHandleAndOriginalArguments(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	controller := newBackgroundTaskController()
	runtime := newDetachTestRuntime(controller)
	runtime.profile = topLevelExecutionProfile()
	registry, err := runtime.promptToolRegistry(t.Context(), directory, backgroundTestOwner)
	require.NoError(t, err)

	arguments := mustArguments(t, `{"background":{"arguments":{"path":"input.txt"},"timeout_seconds":30}}`)
	events, err := runtime.executeProviderToolCalls(registry, backgroundTestOwner, directory)(
		t.Context(),
		[]ToolCall{{
			Metadata: nil, ID: "call-background", Name: string(tool.NameRead),
			Arguments: arguments, ArgumentsJSON: arguments.String(),
		}},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.False(t, events[0].IsError)
	assert.Contains(t, events[0].Result, controller.entity.Task.ID)
	require.NotNil(t, controller.startRequest)
	assert.Equal(t, string(tool.NameRead), controller.startRequest.Target)
	assert.JSONEq(t, `{"path":"input.txt"}`, controller.startRequest.Arguments.String())
	assert.Equal(t, 30*time.Second, controller.startRequest.Timeout)
	assert.Equal(t, backgroundTestOwner, controller.startRequest.Invocation.OwnerSessionID)
	assert.Equal(t, directory, controller.startRequest.Invocation.CWD)
	assert.Equal(t, "call-background", controller.startRequest.Invocation.WrapperCallID)
	assert.Equal(t, "call-background", controller.startRequest.Invocation.ParentCallID)
	assert.Equal(t, "call-background/target", controller.startRequest.Invocation.ID)
}

func TestExplicitBackgroundProviderCallRunsWrapperAndTargetLifecycle(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	controller := newBackgroundTaskController()
	lifecycle := new(recordingToolLifecycle)
	runtime := newToolExecutorTestRuntime(lifecycle)
	runtime.toolTasks = controller
	runtime.profile = topLevelExecutionProfile()
	registry, err := runtime.promptToolRegistry(t.Context(), directory, backgroundTestOwner)
	require.NoError(t, err)
	arguments := mustArguments(t, `{"background":{"arguments":{"path":"input.txt"}}}`)

	events, err := runtime.executeProviderToolCalls(registry, backgroundTestOwner, directory)(
		t.Context(), []ToolCall{{
			Metadata: nil, ID: "wrapper", Name: string(tool.NameRead),
			Arguments: arguments, ArgumentsJSON: arguments.String(),
		}}, nil,
	)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.NotNil(t, controller.startRequest)
	require.NoError(t, controller.startRequest.Admit(t.Context(), controller.startRequest))
	require.NoError(t, runtime.BackgroundToolCompletion(t.Context(), &tooltask.Completion{
		Err: nil, Arguments: controller.startRequest.Arguments, TaskID: "",
		InvocationID: controller.startRequest.Invocation.ID, WrapperCallID: "", OwnerSessionID: "",
		ParentCallID: controller.startRequest.Invocation.ParentCallID,
		Target:       controller.startRequest.Target, ArgumentsJSON: controller.startRequest.Arguments.String(),
		Result:         tool.TextResult(backgroundTestDone, nil),
		SourceSequence: controller.startRequest.Invocation.SourceSequence,
	}))
	assert.Equal(t, []string{
		"tool_call:wrapper:", "tool_result:wrapper:",
		"tool_call:wrapper/target:wrapper", "tool_result:wrapper/target:wrapper",
	}, lifecycle.events)
}

func TestBackgroundToolGetReturnsCompletedOutcome(t *testing.T) {
	t.Parallel()

	controller := newBackgroundTaskController()
	controller.entity.Task.State = database.TaskSucceeded
	outcome := `{"result":{"content":[{"type":"text","text":"` + backgroundTestDone + `"}]},"is_error":false}`
	controller.entity.OutcomeJSON = &outcome
	executor := backgroundTestExecutor(t, controller, tool.NameRead, backgroundTestOwner)

	result, err := executor.Execute(t.Context(), mustArguments(t, `{"background":{"action":"get","task_id":"`+
		controller.entity.Task.ID+`"}}`))
	require.NoError(t, err)
	assert.Equal(t, outcome, result.Text())
	assert.Contains(t, result.Details, "outcome")
}

func TestBackgroundToolLifecycleRejectsInvalidScopeAndTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		owner     string
		target    tool.Name
		taskID    string
		wantError string
	}{
		{
			name: "malformed UUID", owner: backgroundTestOwner, target: tool.NameRead,
			taskID: "bad", wantError: "canonical UUIDv7",
		},
		{name: "wrong owner", owner: "other", target: tool.NameRead, taskID: "", wantError: "not found"},
		{
			name: "wrong target", owner: backgroundTestOwner, target: tool.NameBash,
			taskID: "", wantError: "belongs to tool",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			controller := newBackgroundTaskController()

			taskID := testCase.taskID
			if taskID == "" {
				taskID = controller.entity.Task.ID
			}

			executor := backgroundTestExecutor(t, controller, testCase.target, testCase.owner)
			_, err := executor.Execute(t.Context(), mustArguments(t, `{"background":{"action":"get","task_id":"`+
				taskID+`"}}`))
			require.ErrorContains(t, err, testCase.wantError)
		})
	}
}

func TestBackgroundToolRejectsTimeoutOverflow(t *testing.T) {
	t.Parallel()

	executor := backgroundTestExecutor(t, newBackgroundTaskController(), tool.NameRead, backgroundTestOwner)
	input := fmt.Sprintf(`{"background":{"arguments":{"path":"input.txt"},"timeout_seconds":%d}}`,
		int64(time.Duration(1<<63-1)/time.Second)+1)
	ctx := withTaskInvocation(t.Context(), &tooltask.Invocation{
		ID: "call", WrapperCallID: "", ParentCallID: "", OwnerSessionID: "", CWD: "",
		InitiatingEntryID: "", SourceSequence: 0,
	})

	_, err := executor.Execute(ctx, mustArguments(t, input))
	requireRuntimeOopsCode(t, err, "background_timeout_overflow")
}

func TestBackgroundToolExecutorErrorContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		configure func(*backgroundTaskController)
		name      string
		input     string
		context   func(context.Context) context.Context
		wantError string
	}{
		{
			configure: nil,
			name:      "start requires invocation metadata",
			input:     `{"background":{"arguments":{"path":"input.txt"}}}`,
			context:   func(ctx context.Context) context.Context { return ctx },
			wantError: "requires assistant invocation metadata",
		},
		{
			name:  "start propagates controller error",
			input: `{"background":{"arguments":{"path":"input.txt"}}}`,
			context: func(ctx context.Context) context.Context {
				return withTaskInvocation(ctx, &tooltask.Invocation{
					ID: "call", WrapperCallID: "", ParentCallID: "", OwnerSessionID: "", CWD: "",
					InitiatingEntryID: "", SourceSequence: 0,
				})
			},
			configure: func(controller *backgroundTaskController) { controller.startErr = errors.New("start failed") },
			wantError: "start failed",
		},
		{
			name:      "get propagates controller error",
			input:     `{"background":{"action":"get","task_id":"%s"}}`,
			context:   func(ctx context.Context) context.Context { return ctx },
			configure: func(controller *backgroundTaskController) { controller.getErr = errors.New("get failed") },
			wantError: "get failed",
		},
		{
			name:    "cancel propagates controller error",
			input:   `{"background":{"action":"cancel","task_id":"%s"}}`,
			context: func(ctx context.Context) context.Context { return ctx },
			configure: func(controller *backgroundTaskController) {
				controller.cancelErr = errors.New("cancel failed")
			},
			wantError: "cancel failed",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			controller := newBackgroundTaskController()
			if testCase.configure != nil {
				testCase.configure(controller)
			}

			input := strings.Replace(testCase.input, "%s", controller.entity.Task.ID, 1)

			executor := backgroundTestExecutor(t, controller, tool.NameRead, backgroundTestOwner)

			_, err := executor.Execute(testCase.context(t.Context()), mustArguments(t, input))
			if testCase.wantError != "" {
				require.ErrorContains(t, err, testCase.wantError)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestBackgroundToolRejectsUnknownLifecycleAction(t *testing.T) {
	t.Parallel()

	controller := newBackgroundTaskController()
	executor := backgroundTestExecutor(t, controller, tool.NameRead, backgroundTestOwner)
	_, err := executor.Execute(t.Context(), mustArguments(t, `{"background":{"action":"other","task_id":"`+
		controller.entity.Task.ID+`"}}`))
	require.ErrorContains(t, err, "unknown background action")
}

func TestBackgroundToolCancelUsesOwnerScope(t *testing.T) {
	t.Parallel()

	controller := newBackgroundTaskController()
	executor := backgroundTestExecutor(t, controller, tool.NameRead, backgroundTestOwner)
	result, err := executor.Execute(t.Context(), mustArguments(t, `{"background":{"action":"cancel","task_id":"`+
		controller.entity.Task.ID+`"}}`))
	require.NoError(t, err)
	assert.Equal(t, backgroundTestOwner, controller.cancelOwner)
	assert.Equal(t, database.TaskCanceled, result.Details[taskStateKey])
}

func backgroundTestExecutor(
	t *testing.T,
	controller ToolTaskController,
	name tool.Name,
	owner string,
) *backgroundToolExecutor {
	t.Helper()

	registry, err := tool.NewRegistryWithTools(t.TempDir(), []tool.Name{name})
	require.NoError(t, err)

	var target tool.Executor

	require.NoError(t, registry.Wrap(name, func(executor tool.Executor) tool.Executor {
		target = executor

		return executor
	}))
	require.NotNil(t, target)

	return &backgroundToolExecutor{
		target: target, controller: controller, admit: nil, cache: new(backgroundDefinitionCache),
		owner: owner, cwd: t.TempDir(),
	}
}
