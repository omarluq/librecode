package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/transcript"
)

type activeWorkflowInspectorStub struct {
	byID map[string]*database.WorkflowRunEntity
	runs []database.WorkflowRunEntity
}

func (stub *activeWorkflowInspectorStub) Get(
	_ context.Context, runID string,
) (*database.WorkflowRunEntity, bool, error) {
	run, found := stub.byID[runID]

	return run, found, nil
}

func (stub *activeWorkflowInspectorStub) List(
	context.Context, string, int,
) ([]database.WorkflowRunEntity, error) {
	return stub.runs, nil
}

func (stub *activeWorkflowInspectorStub) Events(
	context.Context, string, int64, int,
) ([]database.TaskEventEntity, error) {
	return nil, nil
}

func (stub *activeWorkflowInspectorStub) AgentTasks(
	context.Context, string,
) ([]database.WorkflowAgentTaskEntity, error) {
	return nil, nil
}

func TestActiveWorkflowAppearsInAgentSummary(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.workflows = &activeWorkflowInspectorStub{byID: nil, runs: []database.WorkflowRunEntity{
		workflowSummaryRun("active", database.TaskRunning),
		workflowSummaryRun(statusDone, database.TaskSucceeded),
	}}

	app.refreshActiveWorkflows(t.Context())
	require.Len(t, app.activeWorkflows, 1)
	lines := app.renderAgentTaskSummary(80)
	require.NotEmpty(t, lines)
	assert.Contains(t, lines[0].Text, "workflow(active review)")
	assert.True(t, app.hasRunningAgentTasks())
}

func TestWorkflowFailureIsPushedIntoCompletedTurn(t *testing.T) {
	t.Parallel()

	running := workflowSummaryRun("failed-run", database.TaskRunning)
	failed := workflowSummaryRun("failed-run", database.TaskFailed)
	failed.Task.ErrorMessage = "compile failed"
	stub := &activeWorkflowInspectorStub{
		byID: map[string]*database.WorkflowRunEntity{failed.Task.ID: &failed},
		runs: []database.WorkflowRunEntity{failed},
	}
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.workflows = stub
	app.working = true
	app.activeWorkflows = []database.WorkflowRunEntity{running}

	app.refreshActiveWorkflows(t.Context())

	assert.Empty(t, app.activeWorkflows)
	require.Len(t, app.liveAgentCompletions, 1)
	assert.Equal(t, transcript.RoleToolResult, app.liveAgentCompletions[0].Role)
	assert.Contains(t, app.liveAgentCompletions[0].Content, "active review")
	assert.Contains(t, app.liveAgentCompletions[0].Content, "failed-run")
	assert.Contains(t, app.liveAgentCompletions[0].Content, "compile failed")
	require.Len(t, app.hiddenQueuedMessages, 1)
	assert.Contains(t, app.hiddenQueuedMessages[0], "background workflow failed")

	app.refreshActiveWorkflows(t.Context())
	assert.Len(t, app.liveAgentCompletions, 1, "failure must only be delivered once")
}

func TestTerminalWorkflowWithoutFailureDoesNotPushCompletion(t *testing.T) {
	t.Parallel()

	for _, state := range []database.TaskState{database.TaskSucceeded, database.TaskCanceled} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			running := workflowSummaryRun("terminal-run", database.TaskRunning)
			terminal := workflowSummaryRun("terminal-run", state)
			app := newRenderTestApp(t)
			app.sessionID = workflowTestSessionID
			app.workflows = &activeWorkflowInspectorStub{
				byID: map[string]*database.WorkflowRunEntity{terminal.Task.ID: &terminal},
				runs: []database.WorkflowRunEntity{terminal},
			}
			app.activeWorkflows = []database.WorkflowRunEntity{running}

			app.refreshActiveWorkflows(t.Context())

			assert.Empty(t, app.activeWorkflows)
			assert.Empty(t, app.liveAgentCompletions)
			assert.Empty(t, app.hiddenQueuedMessages)
		})
	}
}

func TestTrackStartedWorkflowDeliversImmediateFailureOnce(t *testing.T) {
	t.Parallel()

	failed := workflowSummaryRun("immediate-failure", database.TaskFailed)
	failed.Task.ErrorMessage = "source did not compile"
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.working = true
	app.workflows = &activeWorkflowInspectorStub{
		byID: map[string]*database.WorkflowRunEntity{failed.Task.ID: &failed},
		runs: nil,
	}
	event := &assistant.ToolEvent{
		CallID: "", ParentCallID: "", Sequence: 0, Name: workflowToolName,
		ArgumentsJSON: "", DetailsJSON: `{"run_id":"immediate-failure"}`,
		Result: "", Error: "", IsError: false,
	}

	app.trackStartedWorkflow(t.Context(), event)
	app.trackStartedWorkflow(t.Context(), event)

	assert.Empty(t, app.activeWorkflows)
	require.Len(t, app.liveAgentCompletions, 1)
	assert.Equal(t, transcript.RoleToolResult, app.liveAgentCompletions[0].Role)
	assert.Contains(t, app.liveAgentCompletions[0].Content, "active review")
	assert.Contains(t, app.liveAgentCompletions[0].Content, failed.Task.ID)
	assert.Contains(t, app.liveAgentCompletions[0].Content, failed.Task.ErrorMessage)
	require.Len(t, app.hiddenQueuedMessages, 1)
}

func TestTrackStartedWorkflowUsesToolResultRunID(t *testing.T) {
	t.Parallel()

	run := workflowSummaryRun("queued-run", database.TaskQueued)
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.workflows = &activeWorkflowInspectorStub{
		byID: map[string]*database.WorkflowRunEntity{run.Task.ID: &run}, runs: nil,
	}

	app.trackStartedWorkflow(t.Context(), &assistant.ToolEvent{
		CallID: "", ParentCallID: "", Sequence: 0, Name: workflowToolName,
		ArgumentsJSON: "", DetailsJSON: `{"run_id":" queued-run "}`,
		Result: "", Error: "", IsError: false,
	})

	require.Len(t, app.activeWorkflows, 1)
	assert.Equal(t, run.Task.ID, app.activeWorkflows[0].Task.ID)
	assert.Equal(t, "queued-run", workflowRunIDFromDetails(`{"run_id":" queued-run "}`))
	assert.Empty(t, workflowRunIDFromDetails(`{`))
}

func workflowSummaryRun(id string, state database.TaskState) database.WorkflowRunEntity {
	return database.WorkflowRunEntity{
		Task: database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
			LeaseExpiresAt: nil, ID: id, Kind: database.TaskKindWorkflow, ParentTaskID: "",
			OwnerSessionID: workflowTestSessionID, ConcurrencyKey: "", LeaseOwner: "", State: state,
			Result: "", ErrorCode: "", ErrorMessage: "",
		},
		Name: "active review", Source: "", SourceHash: "", SourceVersion: "", ArgumentsJSON: "",
	}
}
