package terminal

import (
	"context"
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
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	behaviorTaskID  = "task-1"
	behaviorRunning = "running"
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

func TestInspectAndLeaveAgentTaskSession(t *testing.T) {
	t.Parallel()

	connection := newPromptSendTestConnection(t)
	sessions := database.NewSessionRepository(connection)
	parent, err := sessions.CreateSession(t.Context(), t.TempDir(), "parent", "")
	require.NoError(t, err)
	child, err := sessions.CreateSession(t.Context(), parent.CWD, "child", parent.ID)
	require.NoError(t, err)

	task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	task.Task.OwnerSessionID = parent.ID
	task.ChildSessionID = child.ID
	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{behaviorTaskID: &task}, nil)
	runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) { options.Sessions = sessions })
	runtime.SetAgentTaskController(stub)

	app := newRenderTestApp(t)
	app.runtime = runtime
	app.sessionID = parent.ID

	require.NoError(t, app.inspectAgentTask(t.Context(), behaviorTaskID))
	assert.Equal(t, child.ID, app.sessionID)
	assert.Equal(t, []string{parent.ID}, app.agentTaskSessionStack)
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, "inspecting agent task")

	require.NoError(t, app.leaveAgentTaskSession(t.Context()))
	assert.Equal(t, parent.ID, app.sessionID)
	assert.Empty(t, app.agentTaskSessionStack)
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, "returned to parent")
	require.EqualError(t, app.leaveAgentTaskSession(t.Context()), "not inspecting an agent task")

	app.sessionID = "other"
	err = app.inspectAgentTask(t.Context(), behaviorTaskID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
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
	task.Task.OwnerSessionID = "another-session"
	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{behaviorTaskID: &task}, nil)
	app := newAgentTaskBehaviorApp(t, stub)
	items := []tui.ListItem{{Value: behaviorTaskID, Title: "task", Description: "", Meta: ""}}
	app.panel = panel.New(panelAgentTasks, "Agent Tasks", "", items, true)

	handled, err := app.handleAgentTasksPanelKey(t.Context(), tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModNone))
	assert.True(t, handled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
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
