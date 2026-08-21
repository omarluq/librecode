package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/transcript"
)

func workflowSubmittedToolEvent(runID string) *assistant.ToolEvent {
	return &assistant.ToolEvent{
		CallID:        "",
		ParentCallID:  "",
		Sequence:      0,
		Name:          toolDisplayExecute,
		ArgumentsJSON: `{"profile":"durable","source":"package main"}`,
		DetailsJSON: `{"execution":"mvm","profile":"durable","result_kind":"accepted",` +
			`"run_id":"` + runID + `","workflow_task_id":"` + runID + `"}`,
		Result:  "",
		Error:   "",
		IsError: false,
	}
}

// submittedWorkflowSnapshot models the first refresh after a workflow tool
// submission. ListActive excludes a quickly terminal run with no active
// children, but the exact-ID lookup resolves it.
func submittedWorkflowSnapshot(sessionID string, run *database.WorkflowRunEntity) terminalRefreshSnapshot {
	snapshot := newTerminalRefreshSnapshot(sessionID)
	snapshot.ActiveWorkflow = refreshSection([]database.WorkflowRunEntity{})

	snapshot.WorkflowByID = refreshSection(map[string]*database.WorkflowRunEntity{})
	if run != nil {
		snapshot.WorkflowByID.Value[run.Task.ID] = run
	}

	return snapshot
}

func TestWorkflowSubmissionRemainsVisibleAcrossFirstRefresh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		state          database.TaskState
		wantActiveRuns int
		wantDelivered  bool
	}{
		{name: "queued", state: database.TaskQueued, wantActiveRuns: 1, wantDelivered: false},
		{name: "running", state: database.TaskRunning, wantActiveRuns: 1, wantDelivered: false},
		{name: "canceling", state: database.TaskCanceling, wantActiveRuns: 1, wantDelivered: false},
		{name: "succeeded", state: database.TaskSucceeded, wantActiveRuns: 0, wantDelivered: true},
		{name: "failed", state: database.TaskFailed, wantActiveRuns: 0, wantDelivered: true},
		{name: "canceled", state: database.TaskCanceled, wantActiveRuns: 0, wantDelivered: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			run := workflowSummaryRun("submitted-run", testCase.state)
			app := newRenderTestApp(t)
			app.sessionID = workflowTestSessionID

			app.trackSubmittedWorkflow(workflowSubmittedToolEvent(run.Task.ID))
			require.Equal(t, []string{run.Task.ID}, app.pendingWorkflowRunIDs())

			snapshot := submittedWorkflowSnapshot(app.sessionID, &run)
			app.applyTerminalRefreshSnapshot(t.Context(), &snapshot)

			assert.NotContains(t, app.pendingWorkflowRuns, run.Task.ID)

			if testCase.wantActiveRuns > 0 {
				require.Len(t, app.activeWorkflows, testCase.wantActiveRuns)
				assert.Equal(t, run.Task.ID, app.activeWorkflows[0].Task.ID)

				lines := app.renderAgentTaskSummary(80)
				assert.Contains(t, workflowRunIDs(app.activeWorkflows), run.Task.ID)
				assert.NotEmpty(t, lines)
			} else {
				assert.Empty(t, app.activeWorkflows)
			}

			if testCase.wantDelivered {
				assert.Contains(t, app.deliveredAgentTasks, run.Task.ID)
				require.Len(t, app.liveAgentCompletions, 1)
				assert.Contains(t, app.liveAgentCompletions[0].Content, run.Task.ID)
				assert.Equal(t, transcript.RoleToolResult, app.liveAgentCompletions[0].Role)
			} else {
				assert.NotContains(t, app.deliveredAgentTasks, run.Task.ID)
				assert.Empty(t, app.liveAgentCompletions)
			}

			// A second refresh must not duplicate the completion or the entry.
			app.applyTerminalRefreshSnapshot(t.Context(), &snapshot)

			if testCase.wantDelivered {
				assert.Len(t, app.liveAgentCompletions, 1)
			}
		})
	}
}

func TestWorkflowSubmissionCapturedIntoRefreshRequest(t *testing.T) {
	t.Parallel()

	run := workflowSummaryRun("captured-run", database.TaskQueued)
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID

	request := app.captureTerminalRefreshRequest()
	assert.Empty(t, request.TrackedWorkflowIDs)
	assert.Empty(t, request.KnownWorkflowIDs)

	event := workflowSubmittedToolEvent(run.Task.ID)
	event.Name = "unified_execute_outer_name"
	app.trackSubmittedWorkflow(event)
	app.trackSubmittedWorkflow(event)

	request = app.captureTerminalRefreshRequest()
	assert.Equal(t, []string{run.Task.ID}, request.TrackedWorkflowIDs)
	assert.Equal(t, []string{run.Task.ID}, request.KnownWorkflowIDs)
}

func TestWorkflowSubmissionIgnoresErrorAndNonDurableResults(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID

	failedEvent := workflowSubmittedToolEvent("ignored")
	failedEvent.IsError = true
	app.trackSubmittedWorkflow(failedEvent)

	const durableDetails = `"profile":"durable","run_id":"ignored"`

	for _, details := range []string{
		`{"execution":"mvm","profile":"turn","result_kind":"completed"}`,
		`{"execution":"mvm",` + durableDetails +
			`,"result_kind":"failed","workflow_task_id":"ignored"}`,
		`{"execution":"other",` + durableDetails +
			`,"result_kind":"accepted","workflow_task_id":"ignored"}`,
		`{"execution":"mvm",` + durableDetails +
			`,"result_kind":"accepted","workflow_task_id":"different"}`,
		`{"run_id":"legacy-name-coupled-result"}`,
	} {
		event := workflowSubmittedToolEvent("ignored")
		event.DetailsJSON = details
		app.trackSubmittedWorkflow(event)
	}

	app.trackSubmittedWorkflow(workflowSubmittedToolEvent(""))
	assert.Empty(t, app.pendingWorkflowRunIDs())

	foreign := workflowSummaryRun("foreign-submitted", database.TaskRunning)
	foreign.Task.OwnerSessionID = workflowTestForeignSession
	app.trackSubmittedWorkflow(workflowSubmittedToolEvent(foreign.Task.ID))

	foreignSnapshot := submittedWorkflowSnapshot(app.sessionID, &foreign)
	app.applyTerminalRefreshSnapshot(t.Context(), &foreignSnapshot)

	assert.NotContains(t, app.deliveredAgentTasks, foreign.Task.ID)
	assert.Empty(t, app.activeWorkflows)
	assert.Empty(t, app.pendingWorkflowRunIDs())
}

func TestWorkflowSubmissionCleansUpMissingRunIDs(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.trackSubmittedWorkflow(workflowSubmittedToolEvent("missing-run"))

	// A successful lookup that reports the run missing resolves immediately.
	missingSnapshot := submittedWorkflowSnapshot(app.sessionID, nil)
	app.applyTerminalRefreshSnapshot(t.Context(), &missingSnapshot)
	assert.NotContains(t, app.pendingWorkflowRuns, "missing-run")
	assert.Empty(t, app.activeWorkflows)

	// Failed lookups stay retryable a bounded number of refreshes.
	app.trackSubmittedWorkflow(workflowSubmittedToolEvent("flaky-run"))

	for refresh := range pendingWorkflowRetryLimit {
		snapshot := newTerminalRefreshSnapshot(app.sessionID)
		snapshot.ActiveWorkflow = refreshSection([]database.WorkflowRunEntity{})
		snapshot.WorkflowByID = terminalRefreshSection[map[string]*database.WorkflowRunEntity]{
			Value: map[string]*database.WorkflowRunEntity{}, Err: assert.AnError, Valid: false,
		}
		app.applyTerminalRefreshSnapshot(t.Context(), &snapshot)

		if refresh < pendingWorkflowRetryLimit-1 {
			assert.Contains(t, app.pendingWorkflowRuns, "flaky-run")
		}
	}

	assert.NotContains(t, app.pendingWorkflowRuns, "flaky-run")
}

// A later failed lookup must not strand runs that were already loaded
// successfully in the same refresh: terminal entries still deliver their
// completion, while the unresolved ID stays pending for the next refresh.
// partialLookupSnapshot builds a refresh snapshot whose exact-ID lookup
// section is invalid overall but still carries the runs that loaded fine.
func partialLookupSnapshot(runs ...database.WorkflowRunEntity) terminalRefreshSnapshot {
	byID := make(map[string]*database.WorkflowRunEntity, len(runs))
	for i := range runs {
		byID[runs[i].Task.ID] = &runs[i]
	}

	snapshot := newTerminalRefreshSnapshot(workflowTestSessionID)
	snapshot.ActiveWorkflow = refreshSection([]database.WorkflowRunEntity{})
	snapshot.WorkflowByID = terminalRefreshSection[map[string]*database.WorkflowRunEntity]{
		Value: byID,
		Err:   assert.AnError,
		Valid: false,
	}

	return snapshot
}

func TestWorkflowSubmissionReconcilesLoadedRunsDuringPartialLookupFailure(t *testing.T) {
	t.Parallel()

	loaded := workflowSummaryRun("loaded-run", database.TaskSucceeded)
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.trackSubmittedWorkflow(workflowSubmittedToolEvent(loaded.Task.ID))
	app.trackSubmittedWorkflow(workflowSubmittedToolEvent("unresolved-run"))

	// The exact-ID lookup for loaded-run succeeded; the one for
	// unresolved-run failed, invalidating the section as a whole.
	snapshot := partialLookupSnapshot(loaded)
	app.applyTerminalRefreshSnapshot(t.Context(), &snapshot)

	// The successfully loaded terminal run is reconciled despite the invalid
	// section: its completion delivered and it left the pending set.
	assert.Contains(t, app.deliveredAgentTasks, loaded.Task.ID)
	assert.NotContains(t, app.pendingWorkflowRuns, loaded.Task.ID)
	assert.Empty(t, app.activeWorkflows)
	// The unresolved run was never loaded, so it stays pending for retry.
	assert.Contains(t, app.pendingWorkflowRuns, "unresolved-run")
}

// The loop reconciles in sorted ID order, so an unresolved ID sorting first
// must not abort reconciliation of later IDs that loaded successfully:
// z-loaded still delivers its completion while a-unresolved stays pending.
func TestWorkflowSubmissionReconcilesSortedLaterRunsDuringPartialLookupFailure(t *testing.T) {
	t.Parallel()

	loaded := workflowSummaryRun("z-loaded", database.TaskSucceeded)
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.trackSubmittedWorkflow(workflowSubmittedToolEvent("a-unresolved"))
	app.trackSubmittedWorkflow(workflowSubmittedToolEvent(loaded.Task.ID))
	require.Equal(t, []string{"a-unresolved", loaded.Task.ID}, app.pendingWorkflowRunIDs())

	// The exact-ID lookup for a-unresolved failed and sorts first; the one for
	// z-loaded succeeded, so only the loaded run is present.
	snapshot := partialLookupSnapshot(loaded)
	app.applyTerminalRefreshSnapshot(t.Context(), &snapshot)

	// z-loaded sorts after the unresolved ID but is still reconciled: its
	// completion delivered and it left the pending set.
	assert.NotContains(t, app.pendingWorkflowRuns, loaded.Task.ID)
	assert.Empty(t, app.activeWorkflows)
	assert.Contains(t, app.deliveredAgentTasks, loaded.Task.ID)
	// a-unresolved sorts first and was never loaded, so it stays pending.
	assert.Contains(t, app.pendingWorkflowRuns, "a-unresolved")
	assert.NotContains(t, app.deliveredAgentTasks, "a-unresolved")
}

func TestWorkflowSubmissionResolvesListedRunWithoutDuplicateLookup(t *testing.T) {
	t.Parallel()

	run := workflowSummaryRun("listed-run", database.TaskRunning)
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.trackSubmittedWorkflow(workflowSubmittedToolEvent(run.Task.ID))

	// ListActive returned the run, so the exact-ID lookup was skipped.
	snapshot := newTerminalRefreshSnapshot(app.sessionID)
	snapshot.ActiveWorkflow = refreshSection([]database.WorkflowRunEntity{run})
	snapshot.WorkflowByID = refreshSection(map[string]*database.WorkflowRunEntity{})

	app.applyTerminalRefreshSnapshot(t.Context(), &snapshot)

	require.Len(t, app.activeWorkflows, 1)
	assert.Equal(t, run.Task.ID, app.activeWorkflows[0].Task.ID)
	assert.Empty(t, app.pendingWorkflowRunIDs())
}

func TestWorkflowSubmissionResetWithAgentTaskTracking(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.trackSubmittedWorkflow(workflowSubmittedToolEvent("reset-run"))
	require.NotEmpty(t, app.pendingWorkflowRunIDs())

	app.resetAgentTaskTracking()
	assert.Empty(t, app.pendingWorkflowRunIDs())
}

func TestApplyStreamedToolEventTracksWorkflowSubmission(t *testing.T) {
	t.Parallel()

	run := workflowSummaryRun("streamed-run", database.TaskQueued)
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID

	app.applyStreamedToolEvent(t.Context(), workflowSubmittedToolEvent(run.Task.ID))
	assert.Equal(t, []string{run.Task.ID}, app.pendingWorkflowRunIDs())
}
