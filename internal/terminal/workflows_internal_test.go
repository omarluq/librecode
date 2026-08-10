package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const workflowNotFound = "not found"

type workflowPanelInspector struct {
	run           *database.WorkflowRunEntity
	tasks         map[string]*database.AgentTaskEntity
	cancelCall    *workflowCancelCall
	links         []database.WorkflowAgentTaskEntity
	detailCalls   [][]string
	listCalls     int
	getCalls      int
	cancelChanged bool
}

type workflowCancelCall struct {
	ownerID string
	runID   string
}

func newWorkflowPanelInspector() *workflowPanelInspector {
	return &workflowPanelInspector{
		run: nil, tasks: map[string]*database.AgentTaskEntity{}, links: nil, cancelCall: nil,
		detailCalls: nil, listCalls: 0, getCalls: 0, cancelChanged: false,
	}
}

func (stub *workflowPanelInspector) Get(
	context.Context,
	string,
) (*database.WorkflowRunEntity, bool, error) {
	stub.getCalls++

	return stub.run, stub.run != nil, nil
}

func (stub *workflowPanelInspector) List(
	context.Context,
	string,
	int,
) ([]database.WorkflowRunEntity, error) {
	stub.listCalls++
	if stub.run == nil {
		return []database.WorkflowRunEntity{}, nil
	}

	return []database.WorkflowRunEntity{*stub.run}, nil
}

func (stub *workflowPanelInspector) ListActive(
	ctx context.Context,
	ownerID string,
	limit int,
) ([]database.WorkflowRunEntity, error) {
	return stub.List(ctx, ownerID, limit)
}

func (stub *workflowPanelInspector) Events(
	context.Context,
	string,
	int64,
	int,
) ([]database.TaskEventEntity, error) {
	return []database.TaskEventEntity{}, nil
}

func (stub *workflowPanelInspector) AgentTasks(
	context.Context,
	string,
) ([]database.WorkflowAgentTaskEntity, error) {
	return stub.links, nil
}

func (stub *workflowPanelInspector) AgentTask(
	_ context.Context,
	taskID string,
) (*database.AgentTaskEntity, bool, error) {
	task, found := stub.tasks[taskID]

	return task, found, nil
}

func (stub *workflowPanelInspector) AgentTaskDetails(
	_ context.Context,
	runIDs []string,
) ([]database.WorkflowAgentTaskDetail, error) {
	stub.detailCalls = append(stub.detailCalls, append([]string(nil), runIDs...))

	details := make([]database.WorkflowAgentTaskDetail, 0, len(stub.links))
	for index := range stub.links {
		link := stub.links[index]

		task, found := stub.tasks[link.AgentTaskID]
		if !found {
			continue
		}

		details = append(details, database.WorkflowAgentTaskDetail{AgentTask: *task, Link: link})
	}

	return details, nil
}

func seedWorkflowPanelSnapshot(t *testing.T, app *App, stub *workflowPanelInspector) {
	t.Helper()

	if stub == nil || stub.run == nil {
		return
	}

	app.workflowPanelSnapshot = []database.WorkflowRunEntity{*stub.run}
	app.workflowPanelSnapshotValid = true

	details, err := loadWorkflowDetails(t.Context(), stub, []string{stub.run.Task.ID})
	require.NoError(t, err)

	app.workflowProgress = details.ProgressByRun
	app.workflowSteps = details.StepsByRun
	app.workflowDetailSnapshotValid = true
}

func (stub *workflowPanelInspector) Cancel(
	_ context.Context,
	ownerSessionID string,
	runID string,
) (bool, error) {
	stub.cancelCall = &workflowCancelCall{ownerID: ownerSessionID, runID: runID}

	return stub.cancelChanged, nil
}

func TestWorkflowItemsDescribeAgentProgress(t *testing.T) {
	t.Parallel()

	run := workflowSummaryRun("run-1", database.TaskRunning)
	succeeded := behaviorAgentTask("agent-1", database.TaskSucceeded)
	failed := behaviorAgentTask("agent-2", database.TaskFailed)
	stub := newWorkflowPanelInspector()
	stub.run = &run
	stub.links = []database.WorkflowAgentTaskEntity{
		workflowLink(run.Task.ID, succeeded.Task.ID, "", 0),
		workflowLink(run.Task.ID, failed.Task.ID, "", 0),
	}
	stub.tasks = map[string]*database.AgentTaskEntity{
		succeeded.Task.ID: &succeeded,
		failed.Task.ID:    &failed,
	}
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.workflows = stub
	seedWorkflowPanelSnapshot(t, app, stub)

	items := app.workflowItemsFromSnapshot()
	require.Len(t, items, 1)
	assert.Equal(t, workflowRunPrefix+run.Task.ID, items[0].Value)
	assert.Contains(t, items[0].Title, "active review")
	assert.Contains(t, items[0].Description, "2/2 agents")
	assert.Contains(t, items[0].Description, "1 failed")
	assert.Equal(
		t,
		workflowProgress{Total: 2, Succeeded: 1, Failed: 1, Running: 0},
		app.workflowProgress[run.Task.ID],
	)
	assert.Equal(t, [][]string{{run.Task.ID}}, stub.detailCalls)
}

func TestWorkflowPanelNavigation(t *testing.T) {
	t.Parallel()

	run := workflowSummaryRun("run-1", database.TaskRunning)
	child := behaviorAgentTask("agent-1", database.TaskRunning)
	stub := newWorkflowPanelInspector()
	stub.run = &run
	stub.links = []database.WorkflowAgentTaskEntity{
		workflowLink(run.Task.ID, child.Task.ID, "review", 2),
	}
	stub.tasks = map[string]*database.AgentTaskEntity{child.Task.ID: &child}
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.workflows = stub
	seedWorkflowPanelSnapshot(t, app, stub)

	app.openWorkflowsPanel(t.Context())
	require.Equal(t, panelWorkflows, app.selectedPanelKind)
	require.Equal(t, "Workflows", app.panel.Title)

	err := app.applyWorkflowSelection(t.Context(), workflowRunPrefix+run.Task.ID)
	require.NoError(t, err)
	assert.Equal(t, run.Task.ID, app.workflowPanelRunID)
	assert.Equal(t, "Workflow: active review", app.panel.Title)
	items := app.panel.Items()
	require.Len(t, items, 2)
	assert.Equal(t, workflowTaskPrefix+child.Task.ID, items[1].Value)
	assert.Contains(t, items[1].Title, "review[2]")

	err = app.handlePanelKey(t.Context(), tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
	require.NoError(t, err)
	assert.Equal(t, panelWorkflows, app.selectedPanelKind)
	assert.Empty(t, app.workflowPanelRunID)
	assert.Equal(t, "Workflows", app.panel.Title)
}

func TestWorkflowDetailRefreshPreservesSelection(t *testing.T) {
	t.Parallel()

	run := workflowSummaryRun("run-1", database.TaskRunning)
	first := behaviorAgentTask("agent-1", database.TaskRunning)
	second := behaviorAgentTask("agent-2", database.TaskRunning)
	stub := newWorkflowPanelInspector()
	stub.run = &run
	stub.links = []database.WorkflowAgentTaskEntity{
		workflowLink(run.Task.ID, first.Task.ID, "inspect", 0),
		workflowLink(run.Task.ID, second.Task.ID, "review", 0),
	}
	stub.tasks = map[string]*database.AgentTaskEntity{
		first.Task.ID:  &first,
		second.Task.ID: &second,
	}
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.workflows = stub
	seedWorkflowPanelSnapshot(t, app, stub)
	require.NoError(t, app.openWorkflowDetail(t.Context(), run.Task.ID))
	app.panel.SetSelectedIndex(2)

	app.refreshWorkflowsPanel(t.Context())

	selected, hasSelection := app.panel.SelectedValue()
	require.True(t, hasSelection)
	assert.Equal(t, workflowTaskPrefix+second.Task.ID, selected)

	stub.links = stub.links[:1]
	seedWorkflowPanelSnapshot(t, app, stub)

	app.refreshWorkflowsPanel(t.Context())
	selected, hasSelection = app.panel.SelectedValue()
	require.True(t, hasSelection)
	assert.Equal(t, workflowRunPrefix+run.Task.ID, selected)
}

func TestWorkflowPanelCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		detail bool
	}{
		{name: "selected run", detail: false},
		{name: "expanded run", detail: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			run := workflowSummaryRun("run-1", database.TaskRunning)
			stub := newWorkflowPanelInspector()
			stub.run = &run
			stub.cancelChanged = true
			app := newRenderTestApp(t)
			app.sessionID = workflowTestSessionID
			app.workflows = stub
			seedWorkflowPanelSnapshot(t, app, stub)
			app.openWorkflowsPanel(t.Context())

			if test.detail {
				require.NoError(t, app.openWorkflowDetail(t.Context(), run.Task.ID))
			}

			handled, err := app.handleWorkflowsPanelKey(
				t.Context(),
				tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModCtrl),
			)
			require.NoError(t, err)
			assert.True(t, handled)
			require.NotNil(t, stub.cancelCall)
			assert.Equal(t, workflowTestSessionID, stub.cancelCall.ownerID)
			assert.Equal(t, run.Task.ID, stub.cancelCall.runID)
			assert.Contains(t, app.statusMessage, "workflow cancel requested")
		})
	}
}

func TestWorkflowPanelPathsDoNotDuplicateSnapshotQueries(t *testing.T) {
	t.Parallel()

	run := workflowSummaryRun("run-1", database.TaskRunning)
	stub := inspectorWithRun(&run)
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.workflows = stub
	seedWorkflowPanelSnapshot(t, app, stub)
	calls := len(stub.detailCalls)

	app.openWorkflowsPanel(t.Context())
	require.NoError(t, app.openWorkflowDetail(t.Context(), run.Task.ID))
	app.refreshWorkflowsPanel(t.Context())

	assert.Equal(t, 0, stub.listCalls)
	assert.Equal(t, 0, stub.getCalls)
	assert.Len(t, stub.detailCalls, calls)
}

func TestOpenWorkflowDetailRequiresSnapshot(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name           string
		ownerSessionID string
		wantErr        string
		wantPendingRun string
		withRun        bool
		wantRefresh    bool
	}{
		{
			name: "missing run", ownerSessionID: workflowTestSessionID,
			withRun: false, wantErr: workflowNotFound, wantRefresh: false, wantPendingRun: "",
		},
		{
			name: "wrong owner", ownerSessionID: "another-session",
			withRun: true, wantErr: workflowNotFound, wantRefresh: false, wantPendingRun: "",
		},
		{
			name: "details loading", ownerSessionID: workflowTestSessionID,
			withRun: true, wantErr: "loading", wantRefresh: true, wantPendingRun: "run-1",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.sessionID = workflowTestSessionID
			app.refreshInFlight = true
			stub := newWorkflowPanelInspector()
			app.workflows = stub

			if testCase.withRun {
				run := workflowSummaryRun("run-1", database.TaskRunning)
				run.Task.OwnerSessionID = testCase.ownerSessionID
				stub.run = &run
				app.workflowPanelSnapshot = []database.WorkflowRunEntity{run}
				app.workflowPanelSnapshotValid = true
			}

			err := app.openWorkflowDetail(t.Context(), "run-1")
			require.ErrorContains(t, err, testCase.wantErr)
			assert.Equal(t, testCase.wantRefresh, app.refreshPending)
			assert.Equal(t, testCase.wantPendingRun, app.workflowPanelRunID)
			assert.Empty(t, stub.detailCalls)
		})
	}
}

func workflowLink(runID, taskID, nodeKey string, invocationIndex int) database.WorkflowAgentTaskEntity {
	return database.WorkflowAgentTaskEntity{
		CreatedAt: time.Time{}, WorkflowTaskID: runID, AgentTaskID: taskID,
		NodeKey: nodeKey, InvocationIndex: invocationIndex, Sequence: 0,
	}
}

func inspectorWithRun(run *database.WorkflowRunEntity) *workflowPanelInspector {
	stub := newWorkflowPanelInspector()
	stub.run = run

	return stub
}
