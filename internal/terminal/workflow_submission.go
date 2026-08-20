package terminal

import (
	"context"
	"maps"
	"slices"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

// pendingWorkflowRetryLimit bounds how many refreshes keep retrying the exact
// lookup of a submitted run ID after the lookup itself failed. Resolved runs
// never count against it.
const pendingWorkflowRetryLimit = 3

// trackSubmittedWorkflow records the run ID returned by a successful workflow
// tool submission. The ID is reconciled by the next terminal refresh instead of
// a synchronous database read, so a workflow that finishes before that refresh
// still surfaces as a completion instead of vanishing from the submission list.
func (app *App) trackSubmittedWorkflow(event *assistant.ToolEvent) {
	if event == nil || event.IsError || event.Name != workflowToolName {
		return
	}

	runID := workflowRunIDFromDetails(event.DetailsJSON)
	if runID == "" || app.hasActiveWorkflow(runID) {
		return
	}

	if app.pendingWorkflowRuns == nil {
		app.pendingWorkflowRuns = map[string]int{}
	}

	if _, pending := app.pendingWorkflowRuns[runID]; !pending {
		app.pendingWorkflowRuns[runID] = 0
	}
}

// pendingWorkflowRunIDs returns the submitted-but-unobserved run IDs in stable
// order for the refresh request captured on the UI goroutine.
func (app *App) pendingWorkflowRunIDs() []string {
	return slices.Sorted(maps.Keys(app.pendingWorkflowRuns))
}

// reconcilePendingWorkflows applies the exact-ID lookups requested for
// submitted runs. It runs on the UI goroutine after the generic workflow
// application so a run already listed as active needs no second entry.
func (app *App) reconcilePendingWorkflows(ctx context.Context, snapshot *terminalRefreshSnapshot) {
	lookups := snapshot.WorkflowByID

	for _, runID := range app.pendingWorkflowRunIDs() {
		if !lookups.Valid {
			break
		}

		if run, found := lookups.Value[runID]; found {
			app.reconcilePendingWorkflowRun(ctx, runID, run)

			continue
		}

		// A run listed by ListActive was not looked up again by ID. Resolve it
		// from the listed section so its terminal transition still delivers once.
		if run := listedWorkflowRun(snapshot.ActiveWorkflow.Value, runID); run != nil {
			app.reconcilePendingWorkflowRun(ctx, runID, run)

			continue
		}

		// The lookup section is valid but the run is absent from both sources, so
		// the submitted run no longer exists. Discard it instead of retrying.
		app.reconcilePendingWorkflowRun(ctx, runID, nil)
	}

	app.retainPendingWorkflowRuns()
}

func listedWorkflowRun(runs []database.WorkflowRunEntity, runID string) *database.WorkflowRunEntity {
	for index := range runs {
		if runs[index].Task.ID == runID {
			return &runs[index]
		}
	}

	return nil
}

// reconcilePendingWorkflowRun resolves one submitted run. Missing or
// foreign-session runs are discarded; queued, running, and canceling runs join
// the inline workflow list; terminal runs deliver their completion exactly once.
func (app *App) reconcilePendingWorkflowRun(
	ctx context.Context,
	runID string,
	run *database.WorkflowRunEntity,
) {
	if run == nil || run.Task.OwnerSessionID != app.sessionID {
		delete(app.pendingWorkflowRuns, runID)

		return
	}

	if isTerminalAgentTaskState(run.Task.State) {
		app.deliverWorkflowCompletion(ctx, run)
		delete(app.pendingWorkflowRuns, runID)

		return
	}

	if !app.hasActiveWorkflow(runID) {
		app.activeWorkflows = append(app.activeWorkflows, *run)
	}

	delete(app.pendingWorkflowRuns, runID)
}

// retainPendingWorkflowRuns keeps unresolved IDs retryable without blocking the
// UI goroutine. A run that joined the inline list is resolved; a submitted ID
// that could not be resolved yet is retried a bounded number of refreshes so
// pending state cannot grow indefinitely.
func (app *App) retainPendingWorkflowRuns() {
	for runID, attempts := range app.pendingWorkflowRuns {
		if app.hasActiveWorkflow(runID) {
			delete(app.pendingWorkflowRuns, runID)

			continue
		}

		attempts++
		if attempts >= pendingWorkflowRetryLimit {
			delete(app.pendingWorkflowRuns, runID)

			continue
		}

		app.pendingWorkflowRuns[runID] = attempts
	}
}
