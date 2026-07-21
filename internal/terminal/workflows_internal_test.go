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
	getFails      bool
	linksFail     bool
	cancelChanged bool
}

type workflowCancelCall struct {
	ownerID string
	runID   string
}

func newWorkflowPanelInspector() *workflowPanelInspector {
	return &workflowPanelInspector{
		run: nil, tasks: map[string]*database.AgentTaskEntity{}, links: nil, cancelCall: nil,
		detailCalls: nil, getFails: false, linksFail: false, cancelChanged: false,
	}
}

func (stub *workflowPanelInspector) Get(
	context.Context,
	string,
) (*database.WorkflowRunEntity, bool, error) {
	if stub.getFails {
		return nil, false, assert.AnError
	}

	return stub.run, stub.run != nil, nil
}

func (stub *workflowPanelInspector) List(
	context.Context,
	string,
	int,
) ([]database.WorkflowRunEntity, error) {
	if stub.run == nil {
		return []database.WorkflowRunEntity{}, nil
	}

	return []database.WorkflowRunEntity{*stub.run}, nil
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
	if stub.linksFail {
		return nil, assert.AnError
	}

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
	if stub.linksFail {
		return nil, assert.AnError
	}

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

	items, err := app.workflowItems(t.Context())
	require.NoError(t, err)
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

func TestOpenWorkflowDetailRejectsInvalidRuns(t *testing.T) {
	t.Parallel()

	foreign := workflowSummaryRun("foreign", database.TaskRunning)
	foreign.Task.OwnerSessionID = workflowTestForeignSession

	tests := []struct {
		name      string
		workflows workflowInspector
		wantError string
	}{
		{name: "runtime unavailable", workflows: nil, wantError: "not configured"},
		{name: "load failure", workflows: inspectorWithGetError(), wantError: "load workflow"},
		{name: "missing run", workflows: newWorkflowPanelInspector(), wantError: workflowNotFound},
		{name: "foreign run", workflows: inspectorWithRun(&foreign), wantError: workflowNotFound},
		{name: "links failure", workflows: inspectorWithLinksError(), wantError: "list workflow agent task details"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.sessionID = workflowTestSessionID
			app.workflows = test.workflows

			err := app.openWorkflowDetail(t.Context(), "run-1")
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantError)
		})
	}
}

func workflowLink(runID, taskID, nodeKey string, invocationIndex int) database.WorkflowAgentTaskEntity {
	return database.WorkflowAgentTaskEntity{
		CreatedAt: time.Time{}, WorkflowTaskID: runID, AgentTaskID: taskID,
		NodeKey: nodeKey, InvocationIndex: invocationIndex, Sequence: 0,
	}
}

func inspectorWithGetError() *workflowPanelInspector {
	stub := newWorkflowPanelInspector()
	stub.getFails = true

	return stub
}

func inspectorWithRun(run *database.WorkflowRunEntity) *workflowPanelInspector {
	stub := newWorkflowPanelInspector()
	stub.run = run

	return stub
}

func inspectorWithLinksError() *workflowPanelInspector {
	run := workflowSummaryRun("run-1", database.TaskRunning)
	stub := inspectorWithRun(&run)
	stub.linksFail = true

	return stub
}
