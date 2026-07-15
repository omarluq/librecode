package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/transcript"
)

const (
	workflowTestRunID     = "run-1"
	workflowTestSessionID = "session-1"
)

type workflowInspectorStub struct {
	listErr  error
	getRun   *database.WorkflowRunEntity
	runs     []database.WorkflowRunEntity
	events   []database.TaskEventEntity
	children []database.WorkflowAgentTaskEntity
	found    bool
}

func (stub *workflowInspectorStub) Get(
	context.Context,
	string,
) (*database.WorkflowRunEntity, bool, error) {
	return stub.getRun, stub.found, nil
}

func (stub *workflowInspectorStub) Events(
	context.Context,
	string,
	int64,
	int,
) ([]database.TaskEventEntity, error) {
	return stub.events, nil
}

func (stub *workflowInspectorStub) AgentTasks(
	context.Context,
	string,
) ([]database.WorkflowAgentTaskEntity, error) {
	return stub.children, nil
}

func (stub *workflowInspectorStub) List(
	_ context.Context,
	ownerSessionID string,
	limit int,
) ([]database.WorkflowRunEntity, error) {
	if stub.listErr != nil {
		return nil, stub.listErr
	}

	if ownerSessionID != workflowTestSessionID || limit != workflowInspectionLimit {
		return nil, errors.New("unexpected list arguments")
	}

	return stub.runs, nil
}

func TestShowWorkflowsListsCurrentSessionRuns(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.workflows = &workflowInspectorStub{
		getRun: nil, events: nil, children: nil, listErr: nil, found: false,
		runs: []database.WorkflowRunEntity{{
			Task: database.TaskEntity{
				CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil,
				UpdatedAt: time.Date(2026, time.March, 5, 12, 30, 0, 0, time.UTC), LeaseExpiresAt: nil,
				ID: workflowTestRunID, Kind: database.TaskKindWorkflow, ParentTaskID: "",
				OwnerSessionID: workflowTestSessionID, ConcurrencyKey: "", LeaseOwner: "",
				State: database.TaskRunning, Result: "", ErrorCode: "", ErrorMessage: "",
			},
			Source: "source", SourceHash: "hash", SourceVersion: "v1", ArgumentsJSON: "{}",
		}},
	}

	require.NoError(t, app.showWorkflows(t.Context(), ""))

	require.Len(t, app.transcript.History, 1)
	assert.Equal(t, transcript.RoleCustom, app.transcript.History[0].Role)
	assert.Equal(t, "run-1\n  state: running\n  updated: 2026-03-05 12:30:00", app.transcript.History[0].Content)
}

func TestShowWorkflowsInspectsOwnedRun(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	startedAt := time.Date(2026, time.March, 5, 12, 29, 58, 0, time.UTC)
	finishedAt := startedAt.Add(2 * time.Second)
	app.workflows = &workflowInspectorStub{
		runs: nil, events: []database.TaskEventEntity{{
			Event: database.EventEntity{ID: "event-1", Kind: "workflow_started", PayloadJSON: "{}",
				CreatedAt: time.Time{}},
			TaskID: workflowTestRunID, Sequence: 1,
		}}, children: []database.WorkflowAgentTaskEntity{{
			CreatedAt: time.Time{}, WorkflowTaskID: workflowTestRunID, AgentTaskID: "agent-1",
			NodeKey: "inspect", InvocationIndex: 0, Sequence: 1,
		}}, listErr: nil, found: true,
		getRun: &database.WorkflowRunEntity{
			Task: database.TaskEntity{
				CreatedAt: time.Time{}, StartedAt: &startedAt, FinishedAt: &finishedAt,
				UpdatedAt: finishedAt, LeaseExpiresAt: nil,
				ID: workflowTestRunID, Kind: database.TaskKindWorkflow, ParentTaskID: "",
				OwnerSessionID: workflowTestSessionID, ConcurrencyKey: "", LeaseOwner: "",
				State: database.TaskSucceeded, Result: `{"value":{"answer":42},"stdout":"done","stderr":""}`,
				ErrorCode: "", ErrorMessage: "",
			},
			Source: "source", SourceHash: "hash", SourceVersion: "v1", ArgumentsJSON: "{}",
		},
	}

	require.NoError(t, app.showWorkflows(t.Context(), workflowTestRunID))
	require.Len(t, app.transcript.History, 1)
	assert.Equal(t, strings.Join([]string{
		workflowTestRunID,
		"  state: succeeded",
		"  updated: 2026-03-05 12:30:00",
		"  source version: v1",
		"  elapsed: 2s",
		"  children: 1",
		"    inspect[0]: agent-1",
		"  events: 1",
		"    1: workflow_started",
		`  value: {"answer":42}`,
		"  stdout: done",
	}, "\n"), app.transcript.History[0].Content)
}

func TestShowWorkflowsHandlesEmptyStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		inspector workflowInspector
		want      string
	}{
		{name: "unavailable", sessionID: workflowTestSessionID, inspector: nil, want: "workflows: unavailable"},
		{name: "no active session", sessionID: "", inspector: &workflowInspectorStub{
			getRun: nil, runs: nil, listErr: nil, found: false,
			events: nil, children: nil,
		}, want: "workflows: no active session"},
		{name: "no runs", sessionID: workflowTestSessionID, inspector: &workflowInspectorStub{
			getRun: nil, runs: nil, listErr: nil, found: false,
			events: nil, children: nil,
		}, want: "workflows: none"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.sessionID = testCase.sessionID
			app.workflows = testCase.inspector

			require.NoError(t, app.showWorkflows(t.Context(), ""))
			require.Len(t, app.transcript.History, 1)
			assert.Equal(t, testCase.want, app.transcript.History[0].Content)
		})
	}
}
