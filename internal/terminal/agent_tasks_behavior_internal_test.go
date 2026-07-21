package terminal

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/transcript"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	behaviorTaskID   = "task-1"
	behaviorRunning  = "running"
	parentScopeValue = "parent-scope"
)

type agentTaskControllerStub struct {
	listErr     error
	getErr      error
	cancelErr   error
	tasks       map[string]*database.AgentTaskEntity
	subscribe   map[string]chan database.TaskEventEntity
	canceled    map[string]bool
	list        []database.AgentTaskEntity
	cancelCalls []string
	mu          sync.Mutex
}

func (stub *agentTaskControllerStub) SubmitAgentTask(
	context.Context,
	*assistant.AgentTaskRequest,
) (*database.AgentTaskEntity, error) {
	return nil, errors.New("not implemented")
}

func newAgentTaskControllerStub(
	tasks map[string]*database.AgentTaskEntity,
	list []database.AgentTaskEntity,
) *agentTaskControllerStub {
	return &agentTaskControllerStub{
		listErr:     nil,
		getErr:      nil,
		cancelErr:   nil,
		tasks:       tasks,
		subscribe:   nil,
		canceled:    make(map[string]bool),
		list:        list,
		cancelCalls: nil,
		mu:          sync.Mutex{},
	}
}

func agentToolEvent(name, details string, isError bool) *assistant.ToolEvent {
	return &assistant.ToolEvent{
		CallID:        "",
		ParentCallID:  "",
		Sequence:      0,
		Name:          name,
		ArgumentsJSON: "",
		DetailsJSON:   details,
		Result:        "",
		Error:         "",
		IsError:       isError,
	}
}

func (stub *agentTaskControllerStub) Get(_ context.Context, taskID string) (*database.AgentTaskEntity, bool, error) {
	if stub.getErr != nil {
		return nil, false, stub.getErr
	}

	task, found := stub.tasks[taskID]

	return task, found, nil
}

func (stub *agentTaskControllerStub) List(context.Context, string, int) ([]database.AgentTaskEntity, error) {
	return stub.list, stub.listErr
}

func (stub *agentTaskControllerStub) Cancel(
	_ context.Context,
	ownerSessionID, taskID string,
) (*database.TaskEntity, bool, error) {
	stub.cancelCalls = append(stub.cancelCalls, ownerSessionID+"/"+taskID)
	if stub.cancelErr != nil {
		return nil, false, stub.cancelErr
	}

	return &stub.tasks[taskID].Task, true, nil
}

func (stub *agentTaskControllerStub) Await(context.Context, string) (*database.AgentTaskEntity, error) {
	return nil, errors.New("not implemented")
}

func (stub *agentTaskControllerStub) SubscribeAgentTask(
	taskID string,
) (events <-chan database.TaskEventEntity, cancel func()) {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	if stub.subscribe == nil {
		stub.subscribe = map[string]chan database.TaskEventEntity{}
	}

	eventsChannel := stub.subscribe[taskID]
	if eventsChannel == nil {
		eventsChannel = make(chan database.TaskEventEntity)
		stub.subscribe[taskID] = eventsChannel
	}

	return eventsChannel, func() {
		stub.mu.Lock()
		defer stub.mu.Unlock()

		stub.canceled[taskID] = true
	}
}

func (stub *agentTaskControllerStub) wasCanceled(taskID string) bool {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	return stub.canceled[taskID]
}

func newAgentTaskBehaviorApp(t *testing.T, stub *agentTaskControllerStub) *App {
	t.Helper()

	runtime := assistant.NewRuntimeForTest(nil)
	runtime.SetAgentTaskController(stub)

	app := newRenderTestApp(t)
	app.runtime = runtime
	app.sessionID = "owner-session"

	return app
}

func behaviorAgentTask(id string, state database.TaskState) database.AgentTaskEntity {
	task := testAgentTask(state)
	task.Task.ID = id
	task.Task.OwnerSessionID = "owner-session"
	task.Task.CreatedAt = time.Unix(100, 0)
	task.AgentName = "explore"
	task.Prompt = "inspect code"

	return task
}

type agentTaskSessionPair struct {
	connection *sql.DB
	sessions   *database.SessionRepository
	parent     *database.SessionEntity
	child      *database.SessionEntity
}

func newAgentTaskSessionPair(t *testing.T) agentTaskSessionPair {
	t.Helper()

	connection := newPromptSendTestConnection(t)
	sessions := database.NewSessionRepository(connection)
	parent, err := sessions.CreateSession(t.Context(), t.TempDir(), "parent", "")
	require.NoError(t, err)
	child, err := sessions.CreateSession(t.Context(), parent.CWD, "child", parent.ID)
	require.NoError(t, err)

	return agentTaskSessionPair{connection: connection, sessions: sessions, parent: parent, child: child}
}

func TestAgentTaskPureBehavior(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		details string
		want    string
	}{
		{name: "valid", details: `{"task_id":" task-1 "}`, want: behaviorTaskID},
		{name: "invalid", details: `{`, want: ""},
		{name: "missing", details: `{}`, want: ""},
	} {
		t.Run("task id "+testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, agentTaskIDFromDetails(testCase.details))
		})
	}

	agentToolNames := []string{
		agentStartToolName,
		agentStatusToolName,
		agentWaitToolName,
		agentCancelToolName,
		agentListToolName,
	}
	for _, name := range agentToolNames {
		assert.True(t, isAgentManagementTool(name))
	}

	assert.False(t, isAgentManagementTool("read"))
	assert.True(t, isTerminalAgentTaskEvent("task_interrupted"))
	assert.False(t, isTerminalAgentTaskEvent("task_running"))

	assert.Equal(t, agentDefaultDisplayName, agentTaskSummaryLabel(nil))

	task := behaviorAgentTask("id", database.TaskRunning)
	task.AgentName = " "
	task.Prompt = "  inspect\n code "
	assert.Equal(t, "agent(inspect code)", agentTaskSummaryLabel(&task))
	task.Prompt = ""
	assert.Equal(t, "agent", agentTaskSummaryLabel(&task))

	assert.Equal(t, "running  id", taskTitle(&task.Task))
	task.Task.ErrorMessage = " failure\n reason "
	assert.Equal(t, "failure reason", taskDescription(&task.Task))
	task.Task.ErrorMessage = ""
	task.Task.Result = strings.Repeat("界", agentTaskDescriptionLimit+10)
	description := taskDescription(&task.Task)
	assert.Len(t, []rune(description), agentTaskDescriptionLimit)
	assert.True(t, strings.HasSuffix(description, "…"))

	started := time.Unix(110, 0)
	finished := time.Unix(125, 0)
	task.Task.StartedAt = &started
	task.Task.FinishedAt = &finished
	assert.Equal(t, "15s", taskMeta(&task.Task, time.Unix(999, 0)))
}

func TestAgentTaskCompletionFallbacksAndDelivery(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		set  func(*database.AgentTaskEntity)
		want string
	}{
		{
			name: "failure detail",
			set:  func(task *database.AgentTaskEntity) { task.Task.ErrorMessage = "failed detail" },
			want: "failed detail",
		},
		{name: "no result", set: func(*database.AgentTaskEntity) {}, want: "No result was returned."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			task := behaviorAgentTask("done", database.TaskFailed)
			testCase.set(&task)
			completion, completed := agentTaskCompletion(database.TaskRunning, &task)
			require.True(t, completed)
			assert.Contains(t, completion, testCase.want)
		})
	}

	app := newRenderTestApp(t)
	app.working = true
	app.agentTasks = []database.AgentTaskEntity{behaviorAgentTask("done", database.TaskRunning)}
	canceled := false
	app.agentTaskWatches["done"] = func() { canceled = true }
	app.deliverAgentTaskCompletionText(t.Context(), "done", "completion text")
	require.Len(t, app.liveAgentCompletions, 1)
	assert.Empty(t, app.agentTasks)
	assert.True(t, canceled)
	assert.Contains(t, app.statusMessage, "1 agent task(s) finished")
	require.Len(t, app.hiddenQueuedMessages, 1)
	assert.Contains(t, app.hiddenQueuedMessages[0], "completion text")

	app.deliverAgentTaskCompletionText(t.Context(), "done", "duplicate")
	assert.Len(t, app.liveAgentCompletions, 1)
	app.deliverAgentTaskCompletionText(t.Context(), "", "ignored")
	app.deliverAgentTaskCompletion(t.Context(), nil)
}

func TestWorkflowChildAgentTasksAreHiddenAndNotDelivered(t *testing.T) {
	t.Parallel()

	child := behaviorAgentTask("workflow-child", database.TaskRunning)
	child.Task.ParentTaskID = "workflow-run"
	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{child.Task.ID: &child},
		[]database.AgentTaskEntity{child},
	)
	app := newAgentTaskBehaviorApp(t, stub)
	app.working = true

	app.discoverActiveAgentTasks(t.Context())
	assert.Empty(t, app.agentTasks)
	app.activeWorkflows = []database.WorkflowRunEntity{
		workflowSummaryRun("workflow-run", database.TaskRunning),
	}
	lines := app.renderAgentTaskSummary(80)
	require.Len(t, lines, 2)
	assert.Equal(t, pendingToolIndicator+" "+app.workflowSummaryLabel(&app.activeWorkflows[0]), lines[0].Text)
	assert.NotContains(t, lines[0].Text, "STEP")
	assert.NotContains(t, lines[0].Text, "STATUS")
	assert.True(t, app.hasRunningAgentTasks())

	app.trackStartedAgentTask(t.Context(), agentToolEvent("", `{"task_id":"workflow-child"}`, false))
	assert.Empty(t, app.agentTasks)

	completed := child
	completed.Task.State = database.TaskSucceeded
	completed.Task.Result = "workflow-owned result"
	stub.tasks[child.Task.ID] = &completed
	app.deliverAgentTaskCompletionText(t.Context(), child.Task.ID, "workflow-owned result")
	app.deliverAgentTaskCompletion(t.Context(), &completed)

	assert.Empty(t, app.liveAgentCompletions)
	assert.Empty(t, app.hiddenQueuedMessages)
	assert.Contains(t, app.deliveredAgentTasks, child.Task.ID)
}

func TestDiscoverRefreshAndTrackAgentTasks(t *testing.T) {
	t.Parallel()

	running := behaviorAgentTask(behaviorRunning, database.TaskRunning)
	done := behaviorAgentTask("done", database.TaskSucceeded)
	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{behaviorRunning: &running, "done": &done},
		[]database.AgentTaskEntity{running, done},
	)
	app := newAgentTaskBehaviorApp(t, stub)
	app.discoverActiveAgentTasks(t.Context())
	require.Len(t, app.agentTasks, 1)
	assert.Equal(t, behaviorRunning, app.agentTasks[0].Task.ID)
	assert.Equal(t, running.AgentName, app.agentTasks[0].AgentName)
	assert.Equal(t, running.Prompt, app.agentTasks[0].Prompt)
	assert.Contains(t, app.agentTaskWatches, behaviorRunning)

	app.trackStartedAgentTask(t.Context(), agentToolEvent("", `{"task_id":"running"}`, false))
	assert.Len(t, app.agentTasks, 1)

	stub.tasks[behaviorRunning] = &done
	done.Task.ID = behaviorRunning
	done.Task.Result = "finished"
	app.working = true
	app.deliverAgentTaskCompletion(t.Context(), &done)
	assert.Empty(t, app.agentTasks)
	require.Len(t, app.hiddenQueuedMessages, 1)
	assert.Contains(t, app.hiddenQueuedMessages[0], "finished")
	assert.True(t, stub.wasCanceled(behaviorRunning))

	app.runtime = nil
	app.refreshVisibleAgentTasks(t.Context())
	assert.Empty(t, app.agentTasks)
}

func TestRefreshActiveAgentTasksReconcilesMissedCompletion(t *testing.T) {
	t.Parallel()

	running := behaviorAgentTask(behaviorRunning, database.TaskRunning)
	completed := running
	completed.Task.State = database.TaskSucceeded
	completed.Task.Result = "finished after missed event"

	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{behaviorRunning: &completed},
		nil,
	)
	app := newAgentTaskBehaviorApp(t, stub)
	app.working = true
	app.agentTasks = []database.AgentTaskEntity{running}
	app.watchActiveAgentTasks(t.Context())

	app.refreshActiveAgentTasks(t.Context())

	assert.Empty(t, app.agentTasks)
	assert.True(t, stub.wasCanceled(behaviorRunning))
	require.Len(t, app.hiddenQueuedMessages, 1)
	assert.Contains(t, app.hiddenQueuedMessages[0], "finished after missed event")
}

func TestRefreshActiveAgentTasksRetainsOmittedRunningTask(t *testing.T) {
	t.Parallel()

	running := behaviorAgentTask(behaviorRunning, database.TaskRunning)
	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{behaviorRunning: &running},
		nil,
	)
	app := newAgentTaskBehaviorApp(t, stub)
	app.agentTasks = []database.AgentTaskEntity{running}

	app.refreshActiveAgentTasks(t.Context())

	require.Len(t, app.agentTasks, 1)
	assert.Equal(t, behaviorRunning, app.agentTasks[0].Task.ID)
}

func TestApplyAgentToolEventAndWatchCleanup(t *testing.T) {
	t.Parallel()

	running := behaviorAgentTask(behaviorRunning, database.TaskRunning)
	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{behaviorRunning: &running},
		[]database.AgentTaskEntity{running},
	)
	app := newAgentTaskBehaviorApp(t, stub)

	app.applyAgentToolEvent(nil)
	app.applyAgentToolEvent(agentToolEvent(agentStartToolName, "", true))
	assert.Empty(t, app.agentTasks)

	app.applyAgentToolEvent(agentToolEvent(agentStartToolName, `{"task_id":"running"}`, false))
	require.Len(t, app.agentTasks, 1)
	assert.False(t, app.agentTasksRefreshedAt.IsZero())

	updated := running
	updated.Task.UpdatedAt = updated.Task.UpdatedAt.Add(time.Second)
	stub.list = []database.AgentTaskEntity{updated}

	app.applyAgentToolEvent(agentToolEvent(agentStatusToolName, "", false))
	assert.Equal(t, updated.Task.UpdatedAt, app.agentTasks[0].Task.UpdatedAt)

	app.stopAgentTaskWatches()
	assert.Empty(t, app.agentTaskWatches)
	assert.True(t, stub.wasCanceled(behaviorRunning))

	app.runtime = nil
	app.applyAgentToolEvent(agentToolEvent(agentStatusToolName, "", false))
}

func TestTrackStartedTerminalTaskDeliversImmediately(t *testing.T) {
	t.Parallel()

	done := behaviorAgentTask("done", database.TaskSucceeded)
	done.Task.Result = "immediate result"
	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{"done": &done}, nil)
	app := newAgentTaskBehaviorApp(t, stub)
	app.working = true
	app.trackStartedAgentTask(t.Context(), agentToolEvent("", `{"task_id":"done"}`, false))
	require.Len(t, app.liveAgentCompletions, 1)
	assert.Contains(t, app.hiddenQueuedMessages[0], "immediate result")
}

func TestAgentTaskStateAndRunningPredicates(t *testing.T) {
	t.Parallel()

	terminal := []database.TaskState{
		database.TaskSucceeded, database.TaskFailed, database.TaskCanceled, database.TaskInterrupted,
	}
	for _, state := range terminal {
		assert.True(t, isTerminalAgentTaskState(state), state)
	}

	nonterminal := []database.TaskState{
		database.TaskQueued, database.TaskRunning, database.TaskCanceling, database.TaskState("unknown"),
	}
	for _, state := range nonterminal {
		assert.False(t, isTerminalAgentTaskState(state), state)
	}

	app := newRenderTestApp(t)
	assert.False(t, app.hasRunningAgentTasks())
	assert.Nil(t, app.renderAgentTaskSummary(10))
	app.agentTasks = []database.AgentTaskEntity{behaviorAgentTask("done", database.TaskSucceeded)}
	assert.False(t, app.hasRunningAgentTasks())
	app.agentTasks = append(app.agentTasks, behaviorAgentTask(behaviorRunning, database.TaskRunning))
	assert.True(t, app.hasRunningAgentTasks())
}

func TestWatchAgentTaskIgnoresNonterminalAndStopsOnContext(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	events := make(chan database.TaskEventEntity, 1)
	events <- database.TaskEventEntity{
		Event:  database.EventEntity{CreatedAt: time.Time{}, ID: "", Kind: "task_running", PayloadJSON: ""},
		TaskID: "", Sequence: 0,
	}

	close(events)

	canceled := make(chan struct{}, 1)

	app.watchAgentTask(t.Context(), "task", events, func() { canceled <- struct{}{} })
	assert.NotEmpty(t, canceled)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	contextCanceled := false

	app.watchAgentTask(ctx, "task", make(chan database.TaskEventEntity), func() { contextCanceled = true })
	assert.True(t, contextCanceled)
}

func TestAgentTaskPanelItemsRefreshAndCancellation(t *testing.T) {
	t.Parallel()

	task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	task.Task.Result = "working result"
	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{behaviorTaskID: &task},
		[]database.AgentTaskEntity{task},
	)
	app := newAgentTaskBehaviorApp(t, stub)

	items, err := app.agentTaskItems(t.Context())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, behaviorTaskID, items[0].Value)
	assert.Contains(t, items[0].Description, "working result")

	app.openAgentTasksPanel(t.Context())
	assert.Equal(t, panelAgentTasks, app.selectedPanelKind)
	app.refreshAgentTasksPanel(t.Context())
	selected, ok := app.panel.SelectedValue()
	require.True(t, ok)
	assert.Equal(t, behaviorTaskID, selected)

	handled, err := app.handleAgentTasksPanelKey(t.Context(), tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone))
	require.NoError(t, err)
	assert.False(t, handled)
	handled, err = app.handleAgentTasksPanelKey(t.Context(), tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModNone))
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, []string{"owner-session/task-1"}, stub.cancelCalls)
	assert.Equal(t, "cancel requested: task-1", app.statusMessage)

	stub.listErr = errors.New("list failed")

	app.openAgentTasksPanel(t.Context())
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, "list agent tasks")
}

func TestAgentTaskSummaryEnterInspectsSelectedSubagent(t *testing.T) {
	t.Parallel()

	fixture := newAgentTaskSessionPair(t)
	sessions, parent, child := fixture.sessions, fixture.parent, fixture.child

	first := behaviorAgentTask("task-first", database.TaskRunning)
	first.Task.OwnerSessionID = parent.ID
	selected := behaviorAgentTask("task-selected", database.TaskRunning)
	selected.Task.OwnerSessionID = parent.ID
	selected.ChildSessionID = child.ID
	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{
		first.Task.ID:    &first,
		selected.Task.ID: &selected,
	}, nil)
	runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) { options.Sessions = sessions })
	runtime.SetAgentTaskController(stub)

	app := newRenderTestApp(t)
	app.runtime = runtime
	app.sessionID = parent.ID
	app.activeWorkflows = []database.WorkflowRunEntity{
		workflowSummaryRun("workflow before tasks", database.TaskRunning),
	}
	app.agentTasks = []database.AgentTaskEntity{first, selected}

	pressTerminalKey(t, app, tcell.KeyTab, "")
	require.True(t, app.agentTaskSummaryFocused())
	pressTerminalKey(t, app, tcell.KeyDown, "")
	pressTerminalKey(t, app, tcell.KeyDown, "")
	pressTerminalKey(t, app, tcell.KeyEnter, "")

	assert.Equal(t, child.ID, app.sessionID)
	assert.Equal(t, []string{parent.ID}, app.agentTaskSessionStack)
	assert.False(t, app.agentTaskSummaryFocused())
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, "task-selected")
}

func TestInspectAgentTaskWithoutRuntimeDoesNotMutateState(t *testing.T) {
	t.Parallel()

	task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	app := newRenderTestApp(t)
	app.sessionID = task.Task.OwnerSessionID
	snapshot := seedInspectFailureState(app, &task)

	err := app.inspectAgentTask(t.Context(), behaviorTaskID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "load agent task")
	assert.Contains(t, err.Error(), "runtime is not configured")
	snapshot(t)
}

func TestInspectAndLeaveAgentTaskSession(t *testing.T) {
	t.Parallel()

	fixture := newAgentTaskSessionPair(t)
	connection, sessions, parent, child := fixture.connection, fixture.sessions, fixture.parent, fixture.child

	task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	task.Task.OwnerSessionID = parent.ID
	task.ChildSessionID = child.ID
	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{behaviorTaskID: &task}, nil)
	runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) { options.Sessions = sessions })
	runtime.SetAgentTaskController(stub)

	_, err := sessions.AppendMessage(t.Context(), child.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleAssistant,
		Content:   "loaded child transcript",
		Provider:  "child-provider",
		Model:     "child-model",
	})
	require.NoError(t, err)

	settings := database.NewDocumentRepository(connection)
	require.NoError(t, settings.Put(t.Context(), &database.DocumentEntity{
		UpdatedAt: time.Now().UTC(),
		Namespace: sessionSettingsNamespace,
		Key:       child.ID,
		ValueJSON: `{"provider":"child-provider","model":"child-model","theme":"light",` +
			`"scoped_enabled":["child-scope"],"scoped_order":["child-scope"],` +
			`"hide_thinking":true,"tools_expanded":true}`,
	}))

	app := newRenderTestApp(t)
	app.runtime = runtime
	app.settings = settings
	app.cfg = promptSendTestConfig()
	app.cfg.Assistant.Provider = "parent-provider"
	app.cfg.Assistant.Model = "parent-model"
	app.sessionID = parent.ID
	app.addSystemMessage("parent transcript")

	app.agentTasks = []database.AgentTaskEntity{task}
	app.scrollOffset = 12
	app.transcript.Streaming.Blocks = []chatMessage{newChatMessage(transcript.RoleAssistant, "stale stream")}
	app.runningToolBlocks = []runningToolBlock{testRunningToolBlock(testToolRead, "")}
	require.NoError(t, app.inspectAgentTask(t.Context(), behaviorTaskID))
	assert.Empty(t, app.transcript.Streaming.Blocks)
	assert.Empty(t, app.runningToolBlocks)
	assertLoadedChildAgentSession(t, app, parent.ID, child.ID, &task)

	app.refreshVisibleAgentTasks(t.Context())
	assert.Equal(t, []database.AgentTaskEntity{task}, app.agentTasks)

	app.scrollOffset = 8
	app.transcript.Streaming.Blocks = []chatMessage{newChatMessage(transcript.RoleAssistant, "child stream")}
	app.runningToolBlocks = []runningToolBlock{testRunningToolBlock(testToolRead, "")}
	require.NoError(t, app.leaveAgentTaskSession(t.Context()))
	assert.Empty(t, app.transcript.Streaming.Blocks)
	assert.Empty(t, app.runningToolBlocks)
	assert.Zero(t, app.scrollOffset)
	assert.Equal(t, parent.ID, app.sessionID)
	assert.Empty(t, app.agentTaskSessionStack)
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, "returned to parent")
	require.EqualError(t, app.leaveAgentTaskSession(t.Context()), "not inspecting an agent task")

	app.sessionID = "other"
	err = app.inspectAgentTask(t.Context(), behaviorTaskID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the current inspection path")
}

func TestInspectAgentTaskSwitchesBetweenSiblingSessions(t *testing.T) {
	t.Parallel()

	connection := newPromptSendTestConnection(t)
	sessions := database.NewSessionRepository(connection)
	parent, err := sessions.CreateSession(t.Context(), t.TempDir(), "parent", "")
	require.NoError(t, err)
	firstChild, err := sessions.CreateSession(t.Context(), parent.CWD, "first child", parent.ID)
	require.NoError(t, err)
	secondChild, err := sessions.CreateSession(t.Context(), parent.CWD, "second child", parent.ID)
	require.NoError(t, err)

	first := behaviorAgentTask("task-first", database.TaskRunning)
	first.Task.OwnerSessionID = parent.ID
	first.ChildSessionID = firstChild.ID
	second := behaviorAgentTask("task-second", database.TaskRunning)
	second.Task.OwnerSessionID = parent.ID
	second.ChildSessionID = secondChild.ID
	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{
		first.Task.ID:  &first,
		second.Task.ID: &second,
	}, nil)
	runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) { options.Sessions = sessions })
	runtime.SetAgentTaskController(stub)

	app := newRenderTestApp(t)
	app.runtime = runtime
	app.sessionID = parent.ID
	app.agentTasks = []database.AgentTaskEntity{first, second}

	require.NoError(t, app.inspectAgentTask(t.Context(), first.Task.ID))
	assert.Equal(t, firstChild.ID, app.sessionID)
	assert.Equal(t, []string{parent.ID}, app.agentTaskSessionStack)

	pressTerminalKey(t, app, tcell.KeyTab, "")
	require.True(t, app.agentTaskSummaryFocused())
	pressTerminalKey(t, app, tcell.KeyDown, "")
	pressTerminalKey(t, app, tcell.KeyEnter, "")
	assert.Equal(t, secondChild.ID, app.sessionID)
	assert.Equal(t, []string{parent.ID}, app.agentTaskSessionStack)
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, second.Task.ID)

	require.NoError(t, app.leaveAgentTaskSession(t.Context()))
	assert.Equal(t, parent.ID, app.sessionID)
	assert.Empty(t, app.agentTaskSessionStack)
}

func TestInspectAgentTaskRecoversRetainedSummaryOwner(t *testing.T) {
	t.Parallel()

	connection := newPromptSendTestConnection(t)
	sessions := database.NewSessionRepository(connection)
	root, err := sessions.CreateSession(t.Context(), t.TempDir(), "root", "")
	require.NoError(t, err)
	parent, err := sessions.CreateSession(t.Context(), root.CWD, "parent", root.ID)
	require.NoError(t, err)
	firstChild, err := sessions.CreateSession(t.Context(), root.CWD, "first child", parent.ID)
	require.NoError(t, err)
	secondChild, err := sessions.CreateSession(t.Context(), root.CWD, "second child", parent.ID)
	require.NoError(t, err)

	second := behaviorAgentTask("task-second", database.TaskRunning)
	second.Task.OwnerSessionID = parent.ID
	second.ChildSessionID = secondChild.ID
	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{second.Task.ID: &second}, nil)
	runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) { options.Sessions = sessions })
	runtime.SetAgentTaskController(stub)

	app := newRenderTestApp(t)
	app.runtime = runtime
	app.sessionID = firstChild.ID
	app.agentTaskSessionStack = []string{root.ID}
	app.agentTaskSummaryOwnerID = parent.ID
	app.agentTasks = []database.AgentTaskEntity{second}

	require.NoError(t, app.inspectAgentTask(t.Context(), second.Task.ID))
	assert.Equal(t, secondChild.ID, app.sessionID)
	assert.Equal(t, []string{root.ID, parent.ID}, app.agentTaskSessionStack)

	require.NoError(t, app.leaveAgentTaskSession(t.Context()))
	assert.Equal(t, parent.ID, app.sessionID)
	assert.Equal(t, []string{root.ID}, app.agentTaskSessionStack)
}

func assertLoadedChildAgentSession(
	t *testing.T,
	app *App,
	parentID string,
	childID string,
	task *database.AgentTaskEntity,
) {
	t.Helper()
	assert.Equal(t, childID, app.sessionID)
	assert.Zero(t, app.scrollOffset)
	assert.Equal(t, []string{parentID}, app.agentTaskSessionStack)
	assert.Equal(t, []database.AgentTaskEntity{*task}, app.agentTasks)
	assert.NotNil(t, app.renderAgentTaskSummary(80))
	require.Len(t, app.transcript.History, 2)
	assert.Equal(t, "loaded child transcript", app.transcript.History[0].Content)
	assert.Contains(t, app.transcript.History[1].Content, "inspecting agent task")
	assert.Equal(t, "child-provider", app.currentProvider())
	assert.Equal(t, "child-model", app.currentModel())
	assert.Equal(t, themeNameLight, app.theme.name)
	assert.True(t, app.hideThinking)
	assert.True(t, app.toolsExpanded)
	assert.Equal(t, []string{"child-scope"}, app.scopedOrder)
	assert.True(t, app.scopedEnabled["child-scope"])
}

func TestDoubleEscapeLeavesAgentTaskSession(t *testing.T) {
	t.Parallel()

	fixture := newAgentTaskSessionPair(t)
	sessions, parent, child := fixture.sessions, fixture.parent, fixture.child

	app := newRenderTestApp(t)
	app.runtime = assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
		options.Sessions = sessions
	})
	app.sessionID = child.ID
	app.agentTaskSessionStack = []string{parent.ID}
	app.agentTasks = []database.AgentTaskEntity{behaviorAgentTask(behaviorTaskID, database.TaskRunning)}
	require.True(t, app.focusAgentTaskSummary())

	pressTerminalKey(t, app, tcell.KeyEscape, "")
	assert.Equal(t, child.ID, app.sessionID)
	assert.Contains(t, app.statusMessage, "return to parent session")

	pressTerminalKey(t, app, tcell.KeyEscape, "")
	assert.Equal(t, parent.ID, app.sessionID)
	assert.Empty(t, app.agentTaskSessionStack)
}

func TestAltEscapeLeavesAgentTaskSession(t *testing.T) {
	t.Parallel()

	fixture := newAgentTaskSessionPair(t)
	sessions, parent, child := fixture.sessions, fixture.parent, fixture.child

	app := newRenderTestApp(t)
	app.runtime = assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
		options.Sessions = sessions
	})
	app.sessionID = child.ID
	app.agentTaskSessionStack = []string{parent.ID}

	shouldQuit, err := app.handleKey(
		t.Context(),
		tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModAlt),
	)
	require.NoError(t, err)
	assert.False(t, shouldQuit)
	assert.Equal(t, parent.ID, app.sessionID)
	assert.Empty(t, app.agentTaskSessionStack)
}

func TestInspectAgentTaskRejectsActivePrompt(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = sessionCommandsParentID
	app.activePrompt = newTestActivePrompt(nil)
	app.agentTasks = []database.AgentTaskEntity{behaviorAgentTask(behaviorTaskID, database.TaskRunning)}

	err := app.inspectAgentTask(t.Context(), behaviorTaskID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is active")
	assert.Equal(t, sessionCommandsParentID, app.sessionID)
	assert.Empty(t, app.agentTaskSessionStack)
	assert.Len(t, app.agentTasks, 1)
}

func TestInspectedAgentTaskCompletionUpdatesRetainedSummary(t *testing.T) {
	t.Parallel()

	running := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	finished := running
	finished.Task.State = database.TaskSucceeded
	now := time.Now().UTC()
	finished.Task.FinishedAt = &now

	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{behaviorTaskID: &finished},
		nil,
	)
	app := newAgentTaskBehaviorApp(t, stub)
	app.agentTasks = []database.AgentTaskEntity{running}
	app.agentTaskSessionStack = []string{sessionCommandsParentID}
	watchCanceled := false
	app.agentTaskWatches[behaviorTaskID] = func() { watchCanceled = true }

	app.handlePromptAsyncEvent(t.Context(), asyncTestEvent(
		asyncEventAgentTaskChanged,
		"",
		behaviorTaskID,
		0,
	))

	require.Len(t, app.agentTasks, 1)
	assert.Equal(t, database.TaskSucceeded, app.agentTasks[0].Task.State)
	assert.True(t, watchCanceled)
	assert.NotContains(t, app.agentTaskWatches, behaviorTaskID)
	assert.NotContains(t, app.deliveredAgentTasks, behaviorTaskID)
}

func seedInspectFailureState(app *App, task *database.AgentTaskEntity) func(*testing.T) {
	app.agentTaskSessionStack = []string{"root-session"}
	app.agentTasks = []database.AgentTaskEntity{*task}
	app.activeWorkflows = []database.WorkflowRunEntity{
		workflowSummaryRun("retained workflow", database.TaskRunning),
	}
	app.agentTasksRefreshedAt = time.Now().Add(-time.Minute)
	app.deliveredAgentTasks["delivered-task"] = struct{}{}
	watchCanceled := false
	app.agentTaskWatches[behaviorTaskID] = func() { watchCanceled = true }
	pendingParentID := "pending-parent"
	app.pendingParentID = &pendingParentID
	app.promptHistory = []string{"parent prompt"}
	app.scrollOffset = 7
	app.cfg = promptSendTestConfig()
	app.cfg.Assistant.Provider = "parent-provider"
	app.cfg.Assistant.Model = "parent-model"
	app.cfg.Assistant.ThinkingLevel = "high"
	app.theme = lightTheme()
	app.hideThinking = true
	app.toolsExpanded = true
	app.scopedEnabled = map[string]bool{parentScopeValue: true}
	app.scopedOrder = []string{parentScopeValue}
	app.tokenUsage = model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   128,
		ContextTokens:   41,
		InputTokens:     23,
		OutputTokens:    17,
	}
	app.selection = mouseSelection{
		lastClickUnixNano: 1,
		startX:            1,
		startY:            2,
		endX:              3,
		endY:              4,
		lastClickX:        3,
		lastClickY:        4,
		clickCount:        1,
		active:            true,
	}
	app.addSystemMessage("parent transcript")
	_ = app.transcript.LineCache.lines(app, 40, 0)
	app.transcriptList = transcriptListSelection{MessageIndex: 0, ItemIndex: 1, Active: true}
	app.panel = panel.New(panelAgentTasks, "Agent Tasks", "", nil, true)
	app.selectedPanelKind = panelAgentTasks
	app.mode = modePanel

	return inspectFailureSnapshotWithWatch(app, task, &watchCanceled)
}

func inspectFailureSnapshotWithWatch(
	app *App,
	task *database.AgentTaskEntity,
	watchCanceled *bool,
) func(*testing.T) {
	openPanel := app.panel
	refreshedAt := app.agentTasksRefreshedAt
	pendingParentID := *app.pendingParentID
	promptHistory := append([]string{}, app.promptHistory...)
	history := append([]chatMessage{}, app.transcript.History...)
	tokenUsage := app.tokenUsage
	selection := app.selection
	transcriptList := app.transcriptList
	summarySelection := app.agentTaskSummarySelection
	lineCacheItems := append([]cachedRenderedMessage{}, app.transcript.LineCache.items...)
	lineCacheState := app.transcript.LineCache.state
	provider, modelID, thinkingLevel := app.currentProvider(), app.currentModel(), app.currentThinkingLevel()
	themeName := app.theme.name

	return func(t *testing.T) {
		t.Helper()
		assert.Equal(t, []string{"root-session"}, app.agentTaskSessionStack)
		assert.Equal(t, []database.AgentTaskEntity{*task}, app.agentTasks)
		assert.Len(t, app.activeWorkflows, 1)
		assert.Equal(t, refreshedAt, app.agentTasksRefreshedAt)
		assert.Contains(t, app.deliveredAgentTasks, "delivered-task")
		assert.Contains(t, app.agentTaskWatches, behaviorTaskID)
		assert.False(t, *watchCanceled)
		require.NotNil(t, app.pendingParentID)
		assert.Equal(t, pendingParentID, *app.pendingParentID)
		assert.Equal(t, promptHistory, app.promptHistory)
		assert.Equal(t, 7, app.scrollOffset)
		assert.Equal(t, provider, app.currentProvider())
		assert.Equal(t, modelID, app.currentModel())
		assert.Equal(t, thinkingLevel, app.currentThinkingLevel())
		assert.Equal(t, themeName, app.theme.name)
		assert.True(t, app.hideThinking)
		assert.True(t, app.toolsExpanded)
		assert.Equal(t, []string{parentScopeValue}, app.scopedOrder)
		assert.True(t, app.scopedEnabled[parentScopeValue])
		assert.Equal(t, tokenUsage, app.tokenUsage)
		assert.Equal(t, selection, app.selection)
		assert.Equal(t, transcriptList, app.transcriptList)
		assert.Equal(t, summarySelection, app.agentTaskSummarySelection)
		assert.Equal(t, lineCacheItems, app.transcript.LineCache.items)
		assert.Equal(t, lineCacheState, app.transcript.LineCache.state)
		assert.Same(t, openPanel, app.panel)
		assert.Equal(t, panelAgentTasks, app.selectedPanelKind)
		assert.Equal(t, modePanel, app.mode)
		assert.Equal(t, history, app.transcript.History)
	}
}

func TestInspectAgentTaskLookupFailureDoesNotMutateState(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		getErr  error
		mutate  func(*database.AgentTaskEntity)
		wantErr string
	}{
		{
			name:    "controller error",
			getErr:  errors.New("controller failed"),
			mutate:  func(*database.AgentTaskEntity) {},
			wantErr: "controller failed",
		},
		{
			name:   "wrong owner",
			getErr: nil,
			mutate: func(task *database.AgentTaskEntity) {
				task.Task.OwnerSessionID = "another-session"
			},
			wantErr: "outside the current inspection path",
		},
		{
			name:   "missing task",
			getErr: nil,
			mutate: func(task *database.AgentTaskEntity) {
				task.Task.ID = "missing-task"
			},
			wantErr: workflowNotFound,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
			app := newRenderTestApp(t)
			app.sessionID = task.Task.OwnerSessionID
			snapshot := seedInspectFailureState(app, &task)

			controllerTask := task
			testCase.mutate(&controllerTask)
			stub := newAgentTaskControllerStub(
				map[string]*database.AgentTaskEntity{controllerTask.Task.ID: &controllerTask},
				nil,
			)
			stub.getErr = testCase.getErr
			app.runtime = assistant.NewRuntimeForTest(nil)
			app.runtime.SetAgentTaskController(stub)

			err := app.inspectAgentTask(t.Context(), behaviorTaskID)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
			assert.Equal(t, task.Task.OwnerSessionID, app.sessionID)
			snapshot(t)
		})
	}
}

func TestInspectAgentTaskLoadFailureDoesNotSwitchSession(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		arrangeFail func(*testing.T, *App, *database.SessionRepository, string)
		name        string
	}{
		{
			name: "settings load",
			arrangeFail: func(t *testing.T, app *App, _ *database.SessionRepository, childID string) {
				t.Helper()
				require.NoError(t, app.settings.Put(t.Context(), &database.DocumentEntity{
					UpdatedAt: time.Now().UTC(), Namespace: sessionSettingsNamespace,
					Key: childID, ValueJSON: "[]",
				}))
			},
		},
		{
			name: "messages load",
			arrangeFail: func(t *testing.T, _ *App, sessions *database.SessionRepository, childID string) {
				t.Helper()
				_, err := sessions.AppendMessage(t.Context(), childID, nil, &database.MessageEntity{
					Timestamp: time.Now().UTC(),
					Role:      database.RoleUser,
					Content:   "child message",
					Provider:  "",
					Model:     "",
				})
				require.NoError(t, err)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := newAgentTaskSessionPair(t)
			connection, sessions, parent, child := fixture.connection, fixture.sessions, fixture.parent, fixture.child

			task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
			task.Task.OwnerSessionID = parent.ID
			task.ChildSessionID = child.ID
			stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{behaviorTaskID: &task}, nil)
			runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
				options.Sessions = sessions
			})
			runtime.SetAgentTaskController(stub)

			app := newRenderTestApp(t)
			app.runtime = runtime
			app.settings = database.NewDocumentRepository(connection)
			app.sessionID = parent.ID
			snapshot := seedInspectFailureState(app, &task)

			testCase.arrangeFail(t, app, sessions, child.ID)

			if testCase.name == "messages load" {
				_, err := connection.ExecContext(t.Context(),
					`UPDATE session_messages SET created_at = 'invalid' WHERE session_id = ?`, child.ID)
				require.NoError(t, err)
			}

			err := app.inspectAgentTask(t.Context(), behaviorTaskID)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "load agent session")
			assert.Equal(t, parent.ID, app.sessionID)
			snapshot(t)
		})
	}
}

func TestAgentTaskCommandPaths(t *testing.T) {
	t.Parallel()

	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{}, nil)
	app := newAgentTaskBehaviorApp(t, stub)
	quit, err := app.runSessionCommand(t.Context(), "agents", "", "/agents")
	require.NoError(t, err)
	assert.False(t, quit)
	assert.Equal(t, panelAgentTasks, app.selectedPanelKind)

	app.runtime = assistant.NewRuntimeForTest(nil)
	quit, err = app.runSessionCommand(t.Context(), "agents", "profiles", "/agents profiles")
	require.NoError(t, err)
	assert.False(t, quit)
	assert.Equal(t, "agents: none", app.transcript.History[len(app.transcript.History)-1].Content)
}

func TestRefreshAgentTasksPanelPreservesSelection(t *testing.T) {
	t.Parallel()

	first := behaviorAgentTask("alpha-task", database.TaskRunning)
	second := behaviorAgentTask("bravo-task", database.TaskQueued)
	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{"alpha-task": &first, "bravo-task": &second},
		[]database.AgentTaskEntity{first, second},
	)
	app := newAgentTaskBehaviorApp(t, stub)
	app.panel = panel.New(panelAgentTasks, "Agent Tasks", "", []tui.ListItem{}, true)
	app.selectedPanelKind = panelAgentTasks
	app.refreshAgentTasksPanel(t.Context())
	assert.Equal(t, 0, app.panel.SelectedIndex())

	stub.listErr = errors.New("refresh failed")

	app.refreshAgentTasksPanel(t.Context())
	assert.Contains(t, app.statusMessage, "list agent tasks")
}

func TestAgentTaskRefreshAndCompletionBranchPaths(t *testing.T) {
	t.Parallel()

	running := behaviorAgentTask(behaviorRunning, database.TaskRunning)
	other := behaviorAgentTask("other", database.TaskQueued)
	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{behaviorRunning: &running, "other": &other},
		[]database.AgentTaskEntity{running},
	)
	app := newAgentTaskBehaviorApp(t, stub)

	// An empty inline list discovers tasks, while a populated list refreshes them.
	app.refreshVisibleAgentTasks(t.Context())
	require.Len(t, app.agentTasks, 1)

	updated := running
	updated.Task.UpdatedAt = updated.Task.UpdatedAt.Add(time.Second)
	stub.list = []database.AgentTaskEntity{updated}

	app.refreshVisibleAgentTasks(t.Context())
	assert.Equal(t, updated.Task.UpdatedAt, app.agentTasks[0].Task.UpdatedAt)

	// Completing one task must retain unrelated active tasks in display order.
	app.agentTasks = []database.AgentTaskEntity{running, other}
	app.working = true
	app.deliverAgentTaskCompletionText(t.Context(), behaviorRunning, "done")
	require.Len(t, app.agentTasks, 1)
	assert.Equal(t, "other", app.agentTasks[0].Task.ID)
}

func TestAgentTaskAsyncChangedRefreshesVisibleTasks(t *testing.T) {
	t.Parallel()

	running := behaviorAgentTask(behaviorRunning, database.TaskRunning)
	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{behaviorRunning: &running},
		[]database.AgentTaskEntity{running},
	)
	app := newAgentTaskBehaviorApp(t, stub)

	app.handlePromptAsyncEvent(t.Context(), &asyncEvent{
		Kind: asyncEventAgentTaskChanged, Text: behaviorRunning, Response: nil, ToolCallEvent: nil,
		ToolEvent: nil, Usage: nil, Provider: "", PromptID: 0,
	})
	require.Len(t, app.agentTasks, 1)
	assert.Equal(t, behaviorRunning, app.agentTasks[0].Task.ID)
}

func TestAgentTaskPanelRejectsTaskOwnedByAnotherSession(t *testing.T) {
	t.Parallel()

	task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	task.Task.OwnerSessionID = workflowTestForeignSession
	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{behaviorTaskID: &task}, nil)
	app := newAgentTaskBehaviorApp(t, stub)
	items := []tui.ListItem{{Value: behaviorTaskID, Title: "task", Description: "", Meta: ""}}
	app.panel = panel.New(panelAgentTasks, "Agent Tasks", "", items, true)

	handled, err := app.handleAgentTasksPanelKey(t.Context(), tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModNone))
	assert.True(t, handled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), workflowNotFound)
	assert.Empty(t, stub.cancelCalls)
}

func TestAgentTaskFallbackAndErrorPaths(t *testing.T) {
	t.Parallel()

	running := behaviorAgentTask(behaviorRunning, database.TaskRunning)
	stub := newAgentTaskControllerStub(
		map[string]*database.AgentTaskEntity{behaviorRunning: &running},
		[]database.AgentTaskEntity{running},
	)
	app := newAgentTaskBehaviorApp(t, stub)

	// Missing and unknown IDs fall back to discovery instead of losing active tasks.
	app.trackStartedAgentTask(t.Context(), agentToolEvent("", `{}`, false))
	require.Len(t, app.agentTasks, 1)
	app.agentTasks = nil
	app.trackStartedAgentTask(t.Context(), agentToolEvent("", `{"task_id":"unknown"}`, false))
	require.Len(t, app.agentTasks, 1)

	// List and refresh failures preserve the last known state.
	stub.listErr = errors.New("list failed")
	app.agentTasks = nil
	app.discoverActiveAgentTasks(t.Context())
	assert.Empty(t, app.agentTasks)
	app.agentTasks = []database.AgentTaskEntity{running}
	stub.getErr = errors.New("refresh failed")

	app.refreshActiveAgentTasks(t.Context())
	require.Len(t, app.agentTasks, 1)
	assert.Equal(t, behaviorRunning, app.agentTasks[0].Task.ID)

	completion, completed := agentTaskCompletion(database.TaskSucceeded, &running)
	assert.False(t, completed)
	assert.Empty(t, completion)
	app.deliverAgentTaskCompletion(t.Context(), &running)
	assert.Empty(t, app.liveAgentCompletions)

	app.runtime = nil
	items, err := app.agentTaskItems(t.Context())
	require.NoError(t, err)
	assert.Nil(t, items)
	app.refreshAgentTasksPanel(t.Context()) // no active panel is a no-op
}

func TestAgentProfilesDisplayDefinitionsDiagnosticsAndTools(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, ".librecode", "agents")
	require.NoError(t, os.MkdirAll(directory, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "invalid.md"), []byte("not frontmatter"), 0o600))

	catalog := agent.Load(root)
	app := newRenderTestApp(t)
	app.runtime = assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
		options.Agents = catalog
	})

	app.showAgentProfiles()
	require.NotEmpty(t, app.transcript.History)
	message := app.transcript.History[len(app.transcript.History)-1]
	assert.Contains(t, message.Content, "explore: Fast read-only codebase exploration")
	assert.Contains(t, message.Content, "tools: read, grep, find, ls, ast")
	assert.Contains(t, message.Content, "permissions: deny")
	assert.Contains(t, message.Content, "warning:")
	assert.Equal(t, "none", agentToolNames(nil))
	assert.Equal(t, "read, grep", agentToolNames([]tool.Name{tool.NameRead, tool.NameGrep}))
}

func TestAgentBackCommandPropagatesNotInspectingError(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	quit, err := app.runSessionCommand(t.Context(), "agents", "back", "/agents back")
	assert.False(t, quit)
	require.EqualError(t, err, "not inspecting an agent task")
}

func TestAgentTaskPanelCancellationErrorsAndEmptySelection(t *testing.T) {
	t.Parallel()

	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{}, nil)
	app := newAgentTaskBehaviorApp(t, stub)
	app.panel = panel.New(panelAgentTasks, "Agent Tasks", "", nil, true)

	handled, err := app.handleAgentTasksPanelKey(t.Context(), tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModNone))
	require.NoError(t, err)
	assert.True(t, handled)

	task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	stub.tasks[behaviorTaskID] = &task
	items := []tui.ListItem{{Value: behaviorTaskID, Title: "task", Description: "", Meta: ""}}
	app.panel = panel.New(panelAgentTasks, "Agent Tasks", "", items, true)
	stub.cancelErr = errors.New("cancel failed")
	handled, err = app.handleAgentTasksPanelKey(t.Context(), tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModNone))
	assert.True(t, handled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancel agent task")

	stub.cancelErr = nil
	stub.getErr = errors.New("get failed")
	_, err = app.handleAgentTasksPanelKey(t.Context(), tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModNone))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load agent task")
}
