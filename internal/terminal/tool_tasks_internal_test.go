package terminal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/tooltask"
	"github.com/omarluq/librecode/internal/transcript"
)

type terminalToolTaskController struct {
	cancelErr    error
	listErr      error
	getErr       error
	getResult    *database.ToolTaskEntity
	cancelResult *database.ToolTaskEntity
	getID        string
	listOwner    string
	getOwner     string
	cancelOwner  string
	cancelID     string
	listResult   []database.ToolTaskEntity
	listLimit    int
	found        bool
}

type toolTaskErrorTestCase struct {
	getErr    error
	cancelErr error
	name      string
	args      string
	want      string
}

func newToolTaskErrorTestCase(name, args, want string) toolTaskErrorTestCase {
	var test toolTaskErrorTestCase

	test.name = name
	test.args = args
	test.want = want

	return test
}

func (*terminalToolTaskController) Start(
	context.Context,
	*tooltask.StartRequest,
) (*database.ToolTaskEntity, error) {
	return nil, errors.New("unexpected start")
}

func (stub *terminalToolTaskController) Get(
	_ context.Context,
	owner string,
	taskID string,
) (*database.ToolTaskEntity, bool, error) {
	stub.getOwner, stub.getID = owner, taskID

	return stub.getResult, stub.found, stub.getErr
}

func (stub *terminalToolTaskController) List(
	_ context.Context,
	owner string,
	_ []database.TaskState,
	limit int,
) ([]database.ToolTaskEntity, error) {
	stub.listOwner, stub.listLimit = owner, limit

	return stub.listResult, stub.listErr
}

func (stub *terminalToolTaskController) Cancel(
	_ context.Context,
	owner string,
	taskID string,
) (*database.ToolTaskEntity, bool, error) {
	stub.cancelOwner, stub.cancelID = owner, taskID

	return stub.cancelResult, stub.found, stub.cancelErr
}

func (*terminalToolTaskController) Wait(
	context.Context,
	string,
	string,
) (*database.ToolTaskEntity, error) {
	return nil, errors.New("unexpected wait")
}

func newToolTaskTestApp(t *testing.T, controller assistant.ToolTaskController) *App {
	t.Helper()

	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.runtime = assistant.NewRuntime(&assistant.RuntimeOptions{
		Config: nil, Sessions: nil, Extensions: nil, Cache: nil, Models: nil, Client: nil, Logger: nil,
		SkillsCache: nil, Agents: nil, AgentTasks: nil, WorkflowSubmitter: nil,
		ToolTasks: controller, ToolCoordinator: nil,
	})

	return app
}

func terminalToolTask(id string, state database.TaskState, target string) database.ToolTaskEntity {
	var task database.TaskEntity

	task.ID = id
	task.State = state

	return database.ToolTaskEntity{
		OutcomeVersion: nil, OutcomeJSON: nil,
		Task:          task,
		WrapperCallID: "", OwnerSessionID: workflowTestSessionID, InvocationID: "", CWD: "",
		ParentCallID: "", InitiatingEntryID: "", PolicyJSON: "", DefinitionJSON: "",
		ArgumentsJSON: "", TargetName: target, SourceSequence: 0, TimeoutSeconds: 0,
	}
}

func TestTasksCommandListsTasksThroughSlashCommand(t *testing.T) {
	t.Parallel()

	stub := new(terminalToolTaskController)
	stub.listResult = []database.ToolTaskEntity{
		terminalToolTask("task-1", database.TaskRunning, "bash"),
		terminalToolTask("task-2", database.TaskSucceeded, "read"),
	}
	app := newToolTaskTestApp(t, stub)

	quit, err := app.submitCommand(t.Context(), "/tasks")

	require.NoError(t, err)
	assert.False(t, quit)
	assert.Equal(t, workflowTestSessionID, stub.listOwner)
	assert.Equal(t, toolTaskListLimit, stub.listLimit)
	require.Len(t, app.transcript.History, 1)
	assert.Equal(t, "task-1  running     bash\ntask-2  succeeded   read", app.transcript.History[0].Content)
	assert.Equal(t, stub.listResult, app.toolTasks)
}

func TestDetachForegroundToolValidation(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	require.EqualError(t, app.detachForegroundTool(""), "no detachable foreground tool")

	app.runtime = assistant.NewRuntimeForTest(nil)
	app.runningToolBlocks = []runningToolBlock{{
		Call: assistant.ToolCallEvent{
			ArgumentsJSON: "", ID: "finished", ParentCallID: "", Name: testToolRead,
			Arguments: tool.EmptyArguments(), Sequence: 0,
		},
		StartedAt: time.Time{},
	}}
	require.EqualError(t, app.detachForegroundTool(""), "foreground tool is no longer detachable")
}

func TestTasksCommandEmptyListSetsStatus(t *testing.T) {
	t.Parallel()

	app := newToolTaskTestApp(t, new(terminalToolTaskController))

	require.NoError(t, app.runToolTaskCommand(t.Context(), ""))
	assert.Equal(t, "no durable background tasks", app.statusMessage)
	assert.Empty(t, app.transcript.History)
	assert.Empty(t, app.toolTasks)
}

func TestTasksCommandInspectsTaskAndFormatsOutcome(t *testing.T) {
	t.Parallel()

	outcome := `{"result":{"content":[{"text":"first line"},{"text":""},{"text":"second line"}]}}`
	task := terminalToolTask("task-7", database.TaskFailed, "grep")
	task.Task.ErrorMessage = "permission denied"
	task.OutcomeJSON = &outcome
	stub := new(terminalToolTaskController)
	stub.getResult = &task
	stub.found = true
	app := newToolTaskTestApp(t, stub)

	quit, err := app.submitCommand(t.Context(), "/tasks task-7")

	require.NoError(t, err)
	assert.False(t, quit)
	assert.Equal(t, workflowTestSessionID, stub.getOwner)
	assert.Equal(t, "task-7", stub.getID)
	require.Len(t, app.transcript.History, 1)
	assert.Equal(
		t,
		"task-7  failed      grep\nerror: permission denied\nfirst line\nsecond line",
		app.transcript.History[0].Content,
	)
}

func TestTasksCommandCancelsAndRefreshesTasks(t *testing.T) {
	t.Parallel()

	canceled := terminalToolTask("task-9", database.TaskCanceled, "write")
	stub := new(terminalToolTaskController)
	stub.cancelResult = &canceled
	stub.found = true
	stub.listResult = []database.ToolTaskEntity{canceled}
	app := newToolTaskTestApp(t, stub)

	require.NoError(t, app.runToolTaskCommand(t.Context(), "cancel task-9"))
	assert.Equal(t, workflowTestSessionID, stub.cancelOwner)
	assert.Equal(t, "task-9", stub.cancelID)
	assert.Equal(t, "task task-9 is canceled", app.statusMessage)
	assert.Equal(t, []database.ToolTaskEntity{canceled}, app.toolTasks)
	assert.Equal(t, workflowTestSessionID, stub.listOwner)
}

func TestTasksCommandErrors(t *testing.T) {
	t.Parallel()

	inspectError := newToolTaskErrorTestCase(
		"inspect service error",
		"task-1",
		"inspect task: get tool task: database offline",
	)
	inspectError.getErr = errors.New("database offline")
	cancelError := newToolTaskErrorTestCase(
		"cancel service error",
		"cancel task-1",
		"cancel task: cancel tool task: database offline",
	)
	cancelError.cancelErr = errors.New("database offline")
	tests := []toolTaskErrorTestCase{
		newToolTaskErrorTestCase("too many arguments", "one two", "usage: /tasks [task-id|cancel <task-id>]"),
		newToolTaskErrorTestCase("cancel missing id", "cancel", "usage: /tasks cancel <task-id>"),
		newToolTaskErrorTestCase("inspect not found", "missing", `task "missing" not found`),
		inspectError,
		newToolTaskErrorTestCase("cancel not found", "cancel missing", `task "missing" not found`),
		cancelError,
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stub := new(terminalToolTaskController)
			stub.getErr = test.getErr
			stub.cancelErr = test.cancelErr
			app := newToolTaskTestApp(t, stub)
			require.EqualError(t, app.runToolTaskCommand(t.Context(), test.args), test.want)
		})
	}
}

func TestTasksCommandRequiresRuntimeAndSession(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	require.EqualError(t, app.runToolTaskCommand(t.Context(), ""), "task runtime is unavailable")

	app.runtime = assistant.NewRuntimeForTest(nil)
	require.EqualError(t, app.runToolTaskCommand(t.Context(), ""), "task runtime is unavailable")
}

func TestRefreshToolTasksPreservesSnapshotOnError(t *testing.T) {
	t.Parallel()

	existing := terminalToolTask("existing", database.TaskRunning, "bash")
	stub := new(terminalToolTaskController)
	stub.listErr = errors.New("database offline")
	app := newToolTaskTestApp(t, stub)
	app.toolTasks = []database.ToolTaskEntity{existing}

	require.ErrorContains(t, app.refreshToolTasks(t.Context()), "refresh tool tasks")

	assert.Equal(t, []database.ToolTaskEntity{existing}, app.toolTasks)
}

func TestFormatToolTaskIgnoresInvalidOutcome(t *testing.T) {
	t.Parallel()

	invalid := `{not-json}`
	task := terminalToolTask("task-3", database.TaskSucceeded, "find")
	task.OutcomeJSON = &invalid

	assert.Equal(t, "task-3  succeeded   find", formatToolTask(&task))
}

func TestBackgroundToolCompletionRendersTargetResultOnce(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	completion := &tooltask.Completion{
		Err: nil, Arguments: tool.EmptyArguments(), TaskID: "task-1", InvocationID: "wrapper/target",
		WrapperCallID: "wrapper", ParentCallID: "wrapper", OwnerSessionID: workflowTestSessionID,
		Target: string(tool.NameBash), ArgumentsJSON: `{"command":"printf done"}`,
		Result: tool.TextResult("done", map[string]any{"exit_code": 0}), SourceSequence: 4,
	}

	app.deliverToolTaskCompletion(completion)
	app.deliverToolTaskCompletion(completion)

	require.Len(t, app.transcript.History, 1)
	assert.Equal(t, transcript.RoleToolResult, app.transcript.History[0].Role)
	parsed := parseToolEventContent(app.transcript.History[0].Content, "")
	assert.Contains(t, app.transcript.History[0].Content, "call_id: wrapper/target")
	assert.Contains(t, app.transcript.History[0].Content, "parent_call_id: wrapper\nsequence: 4")
	assert.Equal(t, string(tool.NameBash), parsed.Name)
	assert.JSONEq(t, `{"command":"printf done"}`, parsed.ArgumentsJSON)
	assert.Equal(t, "done", parsed.Output)
	assert.Empty(t, parsed.Error)
	assert.Equal(t, "$ printf done", toolDisplayFromParsedEvent(&parsed).Title)
}

func TestBackgroundToolFailureRendersInLoadedOwnerSession(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = "visible"
	app.sessionViews[workflowTestSessionID] = app.captureSessionView(false)
	completion := &tooltask.Completion{
		Err: errors.New("exit status 1"), Arguments: tool.EmptyArguments(), TaskID: "task-failed",
		InvocationID: "call/target", WrapperCallID: "call", ParentCallID: "call",
		OwnerSessionID: workflowTestSessionID, Target: string(tool.NameBash),
		ArgumentsJSON: `{"command":"false"}`, Result: tool.TextResult("partial output", nil), SourceSequence: 2,
	}

	app.deliverToolTaskCompletion(completion)

	assert.Empty(t, app.transcript.History)
	owner := app.sessionViews[workflowTestSessionID]
	require.Len(t, owner.transcript.History, 1)
	parsed := parseToolEventContent(owner.transcript.History[0].Content, "")
	assert.Equal(t, "exit status 1", parsed.Error)
	assert.Equal(t, toolDisplayError, toolDisplayFromParsedEvent(&parsed).Status)
}

func TestBackgroundToolCompletionForUnloadedSessionRemainsUndelivered(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	completion := &tooltask.Completion{
		Err: nil, TaskID: "task-later", InvocationID: "", WrapperCallID: "", ParentCallID: "",
		OwnerSessionID: "unloaded", Target: string(tool.NameRead), Arguments: tool.EmptyArguments(),
		ArgumentsJSON: `{"path":"README.md"}`, Result: tool.TextResult("contents", nil), SourceSequence: 0,
	}

	app.deliverToolTaskCompletion(completion)

	assert.Empty(t, app.transcript.History)
	assert.NotContains(t, app.deliveredToolTasks, "task-later")
	assert.Contains(t, app.statusMessage, "owner view is unavailable")
}

func TestNewAppStateInitializesEmptyToolTasks(t *testing.T) {
	t.Parallel()

	app := newAppState(nil, new(RunOptions))

	assert.NotNil(t, app.toolTasks)
	assert.Empty(t, app.toolTasks)
}
