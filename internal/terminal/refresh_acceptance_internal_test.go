package terminal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tooltask"
)

const agentTaskFinishedResult = "finished"

func newRefreshTestApp(t *testing.T) (*App, *clipboardScreen) {
	t.Helper()

	screen := newClipboardScreen()
	app := newAppState(screen, &RunOptions{
		Extensions: nil, Resources: nil, Runtime: nil, Workflows: nil,
		Settings: nil, Models: nil, Auth: nil, Config: nil,
		CWD: "", SessionID: testSlashSession,
	})

	return app, screen
}

type blockingRefreshAgentTasks struct {
	*agentTaskControllerStub
	exited chan struct{}
}

func (stub *blockingRefreshAgentTasks) List(
	ctx context.Context, owner string, limit int,
) ([]database.AgentTaskEntity, error) {
	defer close(stub.exited)

	return stub.agentTaskControllerStub.List(ctx, owner, limit)
}

type refreshToolTaskController struct {
	listStarted chan struct{}
	listRelease chan struct{}
	list        []database.ToolTaskEntity
	mu          sync.Mutex
	listCalls   int
}

func (stub *refreshToolTaskController) Start(
	context.Context, *tooltask.StartRequest,
) (*database.ToolTaskEntity, error) {
	return nil, errors.New("not implemented")
}

func (stub *refreshToolTaskController) Get(
	context.Context, string, string,
) (*database.ToolTaskEntity, bool, error) {
	return nil, false, errors.New("not implemented")
}

func (stub *refreshToolTaskController) List(
	ctx context.Context, _ string, _ []database.TaskState, _ int,
) ([]database.ToolTaskEntity, error) {
	stub.mu.Lock()
	stub.listCalls++
	started, release := stub.listStarted, stub.listRelease
	stub.mu.Unlock()

	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}

	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, fmt.Errorf("list refresh tool tasks: %w", ctx.Err())
		}
	}

	return slices.Clone(stub.list), nil
}

func (stub *refreshToolTaskController) Cancel(
	context.Context, string, string,
) (*database.ToolTaskEntity, bool, error) {
	return nil, false, errors.New("not implemented")
}

func (stub *refreshToolTaskController) Wait(
	context.Context, string, string,
) (*database.ToolTaskEntity, error) {
	return nil, errors.New("not implemented")
}

func (stub *refreshToolTaskController) calls() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	return stub.listCalls
}

type refreshWorkflowInspector struct {
	runs           []database.WorkflowRunEntity
	details        []database.WorkflowAgentTaskDetail
	listActiveCall int
	listCall       int
	getCall        int
	detailCall     int
	mu             sync.Mutex
}

func (stub *refreshWorkflowInspector) Get(
	context.Context, string,
) (*database.WorkflowRunEntity, bool, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	stub.getCall++

	return nil, false, nil
}

func (stub *refreshWorkflowInspector) List(
	context.Context, string, int,
) ([]database.WorkflowRunEntity, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	stub.listCall++

	return slices.Clone(stub.runs), nil
}

func (stub *refreshWorkflowInspector) ListActive(
	context.Context, string, int,
) ([]database.WorkflowRunEntity, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	stub.listActiveCall++

	return slices.Clone(stub.runs), nil
}

func (stub *refreshWorkflowInspector) Events(
	context.Context, string, int64, int,
) ([]database.TaskEventEntity, error) {
	return nil, nil
}

func (stub *refreshWorkflowInspector) AgentTasks(
	context.Context, string,
) ([]database.WorkflowAgentTaskEntity, error) {
	return nil, nil
}

func (stub *refreshWorkflowInspector) AgentTask(
	context.Context, string,
) (*database.AgentTaskEntity, bool, error) {
	return nil, false, nil
}

func (stub *refreshWorkflowInspector) AgentTaskDetails(
	_ context.Context, _ []string,
) ([]database.WorkflowAgentTaskDetail, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	stub.detailCall++

	return slices.Clone(stub.details), nil
}

func (stub *refreshWorkflowInspector) Cancel(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestTerminalRefreshCancellationWhileRuntimeLoadIsBlocked(t *testing.T) {
	t.Parallel()

	app, screen := newRefreshTestApp(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	base := newAgentTaskControllerStub(nil, nil)
	base.listStarted = started
	base.listRelease = release
	stub := &blockingRefreshAgentTasks{agentTaskControllerStub: base, exited: make(chan struct{})}
	app.runtime = assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
		options.AgentTasks = stub
	})

	ctx, cancel := context.WithCancel(context.Background())
	app.requestTerminalRefresh(ctx)
	awaitSignal(t, started, "refresh did not enter Runtime.AgentTasks")
	cancel()
	awaitSignal(t, stub.exited, "Runtime.AgentTasks did not return after application cancellation")

	select {
	case event := <-screen.EventQ():
		t.Fatalf("canceled refresh unexpectedly published %T", event)
	default:
	}
}

func TestTerminalRefreshCancellationWinsBlockedPublication(t *testing.T) {
	t.Parallel()

	app, screen := newRefreshTestApp(t)
	for index := 0; index < cap(screen.EventQ()); index++ {
		screen.EventQ() <- tcell.NewEventInterrupt(index)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		postTerminalRefreshResult(ctx, app.screen, &terminalRefreshResult{
			Snapshot: newTerminalRefreshSnapshot(""), SessionID: "", Timing: terminalRefreshTiming{
				Total: 0, AgentTasks: 0, AgentPanel: 0, ToolTasks: 0,
				Workflows: 0, Details: 0, WorkflowPanel: 0,
			},
			Generation: 0, TimedOut: false, Canceled: false,
		})
		close(done)
	}()

	cancel()
	awaitSignal(t, done, "blocked refresh publication did not stop on cancellation")
}

func TestTerminalRefreshLatestStateCoalescing(t *testing.T) {
	t.Parallel()

	app, screen := newRefreshTestApp(t)
	app.runtime = &assistant.Runtime{}
	started := make(chan terminalRefreshRequest, 2)
	release := make(chan struct{}, 2)
	app.refreshLoader = func(ctx context.Context, request *terminalRefreshRequest) terminalRefreshSnapshot {
		started <- *request

		select {
		case <-release:
		case <-ctx.Done():
		}

		return newTerminalRefreshSnapshot(request.SessionID)
	}

	app.requestTerminalRefresh(t.Context())
	first := awaitRequest(t, started)
	require.False(t, first.LoadAgentPanel)

	app.selectedPanelKind = panelAgentTasks
	app.agentTasks = []database.AgentTaskEntity{behaviorAgentTask("latest-task", database.TaskRunning)}
	app.requestTerminalRefresh(t.Context())
	app.requestTerminalRefresh(t.Context())

	release <- struct{}{}

	applyNextRefreshInterrupt(t, app, screen)
	second := awaitRequest(t, started)
	assert.True(t, second.LoadAgentPanel)
	assert.Equal(t, []string{"latest-task"}, second.TrackedAgentTaskIDs)
	assert.True(t, app.refreshInFlight)
	assert.False(t, app.refreshPending)

	release <- struct{}{}

	applyNextRefreshInterrupt(t, app, screen)
}

func TestTerminalRefreshSessionTransitionDiscardsInFlightSnapshot(t *testing.T) {
	t.Parallel()

	app, screen := newRefreshTestApp(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	oldTask := behaviorAgentTask("old-task", database.TaskRunning)
	stub := newAgentTaskControllerStub(nil, []database.AgentTaskEntity{oldTask})
	stub.listStarted = started
	stub.listRelease = release
	app.runtime = assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
		options.AgentTasks = stub
	})

	app.requestTerminalRefresh(t.Context())
	awaitSignal(t, started, "old-session load did not start")
	close(release)

	interrupt := awaitInterrupt(t, screen.EventQ(), "old-session result was not published")

	app.sessionID = "new-session"
	app.resetAgentTaskTracking()
	_, err := app.handleInterrupt(t.Context(), interrupt)
	require.NoError(t, err)

	assert.Equal(t, "new-session", app.sessionID)
	assert.Empty(t, app.agentTasks)
	assert.False(t, app.refreshInFlight)
	assert.False(t, app.agentTaskPanelSnapshotValid)
}

func TestTerminalRefreshPartialSnapshotPreservesEveryFailedSection(t *testing.T) {
	t.Parallel()

	app, _ := newRefreshTestApp(t)
	oldAgent := behaviorAgentTask("old-agent", database.TaskRunning)
	newAgent := behaviorAgentTask("new-agent", database.TaskQueued)
	oldPanel := behaviorAgentTask("old-panel", database.TaskRunning)
	oldTool := refreshToolTask("old-tool")
	oldRun := workflowSummaryRun("old-run", database.TaskRunning)
	oldPanelRun := workflowSummaryRun("old-panel-run", database.TaskRunning)

	app.agentTasks = []database.AgentTaskEntity{oldAgent}
	app.agentTaskPanelSnapshot = []database.AgentTaskEntity{oldPanel}
	app.agentTaskPanelSnapshotValid = true
	app.toolTasks = []database.ToolTaskEntity{oldTool}
	app.activeWorkflows = []database.WorkflowRunEntity{oldRun}
	app.workflowPanelSnapshot = []database.WorkflowRunEntity{oldPanelRun}
	app.workflowPanelSnapshotValid = true
	app.workflowProgress[oldRun.Task.ID] = workflowProgress{
		Total: 3, Succeeded: 0, Failed: 0, Running: 1,
	}
	app.workflowSteps[oldRun.Task.ID] = []database.WorkflowAgentTaskDetail{{
		AgentTask: testAgentTask(database.TaskRunning), Link: refreshWorkflowLink("", "", ""),
	}}
	app.workflowDetailSnapshotValid = true

	snapshot := newTerminalRefreshSnapshot(app.sessionID)
	snapshot.AgentTasks = refreshSection([]database.AgentTaskEntity{newAgent})
	snapshot.AgentTaskByID = refreshSection(map[string]*database.AgentTaskEntity{})
	snapshot.AgentPanel.Err = assert.AnError
	snapshot.ToolTasks.Err = assert.AnError
	snapshot.ActiveWorkflow.Err = assert.AnError
	snapshot.WorkflowPanel.Err = assert.AnError
	snapshot.WorkflowDetail.Err = assert.AnError

	app.applyTerminalRefreshSnapshot(t.Context(), &snapshot)

	require.Len(t, app.agentTasks, 1)
	assert.Equal(t, "new-agent", app.agentTasks[0].Task.ID)
	assert.Equal(t, "old-panel", app.agentTaskPanelSnapshot[0].Task.ID)
	assert.Equal(t, "old-tool", app.toolTasks[0].Task.ID)
	assert.Equal(t, "old-run", app.activeWorkflows[0].Task.ID)
	assert.Equal(t, "old-panel-run", app.workflowPanelSnapshot[0].Task.ID)
	assert.Equal(t, 3, app.workflowProgress[oldRun.Task.ID].Total)
	assert.Len(t, app.workflowSteps[oldRun.Task.ID], 1)
	assert.True(t, app.workflowDetailSnapshotValid)
}

func TestTerminalRefreshApplySideEffectsOnlyDuringInterrupt(t *testing.T) {
	t.Parallel()

	app, screen := newRefreshTestApp(t)
	running := behaviorAgentTask("completion", database.TaskRunning)
	completed := behaviorAgentTask("completion", database.TaskSucceeded)
	completed.Task.OwnerSessionID = app.sessionID
	completed.Task.Result = agentTaskFinishedResult
	app.agentTasks = []database.AgentTaskEntity{running}
	app.working = true
	watchStopped := false
	app.agentTaskWatches[running.Task.ID] = func() { watchStopped = true }
	app.runtime = &assistant.Runtime{}
	app.refreshLoader = func(context.Context, *terminalRefreshRequest) terminalRefreshSnapshot {
		snapshot := newTerminalRefreshSnapshot(app.sessionID)
		snapshot.AgentTasks = refreshSection([]database.AgentTaskEntity{completed})
		snapshot.AgentTaskByID = refreshSection(map[string]*database.AgentTaskEntity{
			completed.Task.ID: &completed,
		})
		snapshot.AgentPanel = refreshSection([]database.AgentTaskEntity{completed})

		return snapshot
	}

	app.requestTerminalRefresh(t.Context())
	interrupt := awaitInterrupt(t, screen.EventQ(), "refresh result was not published")
	assert.NotContains(t, app.deliveredAgentTasks, completed.Task.ID)
	assert.False(t, watchStopped)
	assert.False(t, app.agentTaskPanelSnapshotValid)

	_, err := app.handleInterrupt(t.Context(), interrupt)
	require.NoError(t, err)
	assert.Contains(t, app.deliveredAgentTasks, completed.Task.ID)
	assert.True(t, watchStopped)
	assert.True(t, app.agentTaskPanelSnapshotValid)
}

func TestLoadTerminalRefreshSnapshotAssociatesWorkflowDetailsWithRuns(t *testing.T) {
	t.Parallel()

	runA := workflowSummaryRun("run-a", database.TaskRunning)
	runB := workflowSummaryRun("run-b", database.TaskRunning)
	taskA := behaviorAgentTask("task-a", database.TaskSucceeded)
	taskB := behaviorAgentTask("task-b", database.TaskRunning)
	workflows := &refreshWorkflowInspector{
		runs: []database.WorkflowRunEntity{runA, runB},
		details: []database.WorkflowAgentTaskDetail{
			{AgentTask: taskB, Link: refreshWorkflowLink(runB.Task.ID, taskB.Task.ID, "")},
			{AgentTask: taskA, Link: refreshWorkflowLink(runA.Task.ID, taskA.Task.ID, "")},
		},
		listActiveCall: 0, listCall: 0, getCall: 0, detailCall: 0, mu: sync.Mutex{},
	}

	snapshot := loadTerminalRefreshSnapshot(t.Context(), &terminalRefreshRequest{
		Workflows: workflows, Runtime: nil, SessionID: testSlashSession,
		WorkflowDetailID: runB.Task.ID, TrackedAgentTaskIDs: nil,
		TrackedWorkflowIDs: []string{runA.Task.ID}, KnownWorkflowIDs: []string{runB.Task.ID},
		LoadAgentPanel: false, LoadWorkflowPanel: true,
	})

	require.True(t, snapshot.WorkflowDetail.Valid)
	assert.Equal(t, 1, snapshot.WorkflowDetail.Value.ProgressByRun[runA.Task.ID].Succeeded)
	assert.Equal(t, 1, snapshot.WorkflowDetail.Value.ProgressByRun[runB.Task.ID].Running)
	require.Len(t, snapshot.WorkflowDetail.Value.StepsByRun[runA.Task.ID], 1)
	require.Len(t, snapshot.WorkflowDetail.Value.StepsByRun[runB.Task.ID], 1)
	assert.Equal(t, "task-a", snapshot.WorkflowDetail.Value.StepsByRun[runA.Task.ID][0].AgentTask.Task.ID)
	assert.Equal(t, "task-b", snapshot.WorkflowDetail.Value.StepsByRun[runB.Task.ID][0].AgentTask.Task.ID)
}

func TestTerminalPanelsUsePublishedSnapshotsWithoutDuplicateQueries(t *testing.T) {
	t.Parallel()

	agent := behaviorAgentTask("agent", database.TaskRunning)
	run := workflowSummaryRun("run", database.TaskRunning)
	run.Task.OwnerSessionID = testSlashSession
	child := behaviorAgentTask("child", database.TaskSucceeded)
	agentStub := newAgentTaskControllerStub(nil, []database.AgentTaskEntity{agent})
	toolStub := &refreshToolTaskController{
		listStarted: nil, listRelease: nil, list: nil, mu: sync.Mutex{}, listCalls: 0,
	}
	workflowStub := &refreshWorkflowInspector{
		runs: []database.WorkflowRunEntity{run},
		details: []database.WorkflowAgentTaskDetail{{
			AgentTask: child,
			Link:      refreshWorkflowLink(run.Task.ID, child.Task.ID, "build"),
		}},
		listActiveCall: 0, listCall: 0, getCall: 0, detailCall: 0, mu: sync.Mutex{},
	}
	app, _ := newRefreshTestApp(t)
	app.runtime = assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
		options.AgentTasks = agentStub
		options.ToolTasks = toolStub
	})
	app.workflows = workflowStub
	run.Task.OwnerSessionID = app.sessionID
	workflowStub.runs[0] = run

	snapshot := loadTerminalRefreshSnapshot(t.Context(), &terminalRefreshRequest{
		Runtime: app.runtime, Workflows: app.workflows, SessionID: app.sessionID,
		WorkflowDetailID: run.Task.ID, TrackedAgentTaskIDs: nil, TrackedWorkflowIDs: nil,
		KnownWorkflowIDs: nil, LoadAgentPanel: true, LoadWorkflowPanel: true,
	})
	app.applyTerminalRefreshSnapshot(t.Context(), &snapshot)

	agentCalls := agentStub.listCalls
	toolCalls := toolStub.calls()

	workflowStub.mu.Lock()

	workflowCalls := []int{
		workflowStub.listActiveCall, workflowStub.listCall,
		workflowStub.getCall, workflowStub.detailCall,
	}
	workflowStub.mu.Unlock()

	app.openAgentTasksPanel(t.Context())
	app.refreshAgentTasksPanel(t.Context())
	app.openWorkflowsPanel(t.Context())
	require.NoError(t, app.openWorkflowDetail(t.Context(), run.Task.ID))
	app.refreshWorkflowsPanel(t.Context())

	assert.Equal(t, agentCalls, agentStub.listCalls)
	assert.Equal(t, toolCalls, toolStub.calls())

	workflowStub.mu.Lock()
	assert.Equal(t, []int{
		workflowStub.listActiveCall, workflowStub.listCall, workflowStub.getCall, workflowStub.detailCall,
	}, workflowCalls)
	workflowStub.mu.Unlock()
	require.NotNil(t, app.panel)
	assert.Equal(t, panelWorkflows, app.panel.Kind)
	assert.Len(t, app.panel.Items(), 2)
}

func awaitSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

func refreshSection[T any](value T) terminalRefreshSection[T] {
	return terminalRefreshSection[T]{Value: value, Err: nil, Valid: true}
}

func refreshWorkflowLink(runID, taskID, node string) database.WorkflowAgentTaskEntity {
	return database.WorkflowAgentTaskEntity{
		CreatedAt: time.Time{}, WorkflowTaskID: runID, AgentTaskID: taskID,
		NodeKey: node, InvocationIndex: 0, Sequence: 0,
	}
}

func refreshToolTask(id string) database.ToolTaskEntity {
	return database.ToolTaskEntity{
		OutcomeVersion: nil, OutcomeJSON: nil, Task: refreshTask(id),
		WrapperCallID: "", OwnerSessionID: "", InvocationID: "", CWD: "", ParentCallID: "",
		InitiatingEntryID: "", PolicyJSON: "", DefinitionJSON: "", ArgumentsJSON: "",
		TargetName: "", SourceSequence: 0, TimeoutSeconds: 0,
	}
}

func refreshTask(id string) database.TaskEntity {
	return database.TaskEntity{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
		LeaseExpiresAt: nil, ID: id, Kind: database.TaskKindTool, ParentTaskID: "",
		OwnerSessionID: "", ConcurrencyKey: "", LeaseOwner: "", State: database.TaskQueued,
		Result: "", ErrorCode: "", ErrorMessage: "",
	}
}

func awaitRequest(t *testing.T, requests <-chan terminalRefreshRequest) terminalRefreshRequest {
	t.Helper()

	select {
	case request := <-requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("refresh request was not captured")

		return terminalRefreshRequest{
			Workflows: nil, Runtime: nil, SessionID: "", WorkflowDetailID: "",
			TrackedAgentTaskIDs: nil, TrackedWorkflowIDs: nil, KnownWorkflowIDs: nil,
			LoadAgentPanel: false, LoadWorkflowPanel: false,
		}
	}
}

func awaitInterrupt(t *testing.T, events <-chan tcell.Event, failure string) *tcell.EventInterrupt {
	t.Helper()

	select {
	case event := <-events:
		interrupt, ok := event.(*tcell.EventInterrupt)
		require.True(t, ok)

		return interrupt
	case <-time.After(time.Second):
		t.Fatal(failure)

		return nil
	}
}

func applyNextRefreshInterrupt(t *testing.T, app *App, screen *clipboardScreen) {
	t.Helper()
	interrupt := awaitInterrupt(t, screen.EventQ(), "refresh interrupt was not published")
	_, err := app.handleInterrupt(t.Context(), interrupt)
	require.NoError(t, err)
}
