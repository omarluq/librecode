package terminal

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

// terminalRefreshRequest is a value-only description of one database load. It
// is safe to pass to a worker: none of its collections alias App state.
type terminalRefreshRequest struct {
	Workflows           workflowInspector
	Runtime             *assistant.Runtime
	SessionID           string
	WorkflowDetailID    string
	TrackedAgentTaskIDs []string
	TrackedWorkflowIDs  []string
	KnownWorkflowIDs    []string
	LoadAgentPanel      bool
	LoadWorkflowPanel   bool
}

type terminalRefreshSection[T any] struct {
	Value T
	Err   error
	Valid bool
}

// terminalRefreshSnapshot is immutable once returned by loadTerminalRefreshSnapshot.
// Sections are independent so a failed query never invalidates successful data.
type terminalRefreshSnapshot struct {
	AgentTasks     terminalRefreshSection[[]database.AgentTaskEntity]
	AgentPanel     terminalRefreshSection[[]database.AgentTaskEntity]
	ToolTasks      terminalRefreshSection[[]database.ToolTaskEntity]
	ActiveWorkflow terminalRefreshSection[[]database.WorkflowRunEntity]
	WorkflowPanel  terminalRefreshSection[[]database.WorkflowRunEntity]
	WorkflowDetail terminalRefreshSection[workflowDetails]
	AgentTaskByID  terminalRefreshSection[map[string]*database.AgentTaskEntity]
	WorkflowByID   terminalRefreshSection[map[string]*database.WorkflowRunEntity]
	SessionID      string
	Timing         terminalRefreshTiming
}

func (app *App) captureTerminalRefreshRequest() terminalRefreshRequest {
	agentIDs := make([]string, len(app.agentTasks))
	for index := range app.agentTasks {
		agentIDs[index] = app.agentTasks[index].Task.ID
	}

	workflowIDs := make([]string, 0, len(app.activeWorkflows)+len(app.workflowPanelSnapshot)+1)
	knownWorkflowIDs := make([]string, 0, len(app.activeWorkflows)+len(app.workflowPanelSnapshot)+1)
	known := make(map[string]struct{})

	for index := range app.activeWorkflows {
		workflowIDs = append(workflowIDs, app.activeWorkflows[index].Task.ID)
		known[workflowIDs[len(workflowIDs)-1]] = struct{}{}
		knownWorkflowIDs = append(knownWorkflowIDs, workflowIDs[len(workflowIDs)-1])
	}

	// Submitted runs that have not been observed yet are tracked exactly so the
	// refresh worker can resolve them even when ListActive already excludes a
	// quickly terminal run.
	for _, runID := range app.pendingWorkflowRunIDs() {
		if _, found := known[runID]; found {
			continue
		}

		workflowIDs = append(workflowIDs, runID)
		known[runID] = struct{}{}
		knownWorkflowIDs = append(knownWorkflowIDs, runID)
	}

	for index := range app.workflowPanelSnapshot {
		id := app.workflowPanelSnapshot[index].Task.ID
		if _, found := known[id]; !found {
			known[id] = struct{}{}
			knownWorkflowIDs = append(knownWorkflowIDs, id)
		}
	}

	if app.workflowPanelRunID != "" {
		if _, found := known[app.workflowPanelRunID]; !found {
			knownWorkflowIDs = append(knownWorkflowIDs, app.workflowPanelRunID)
		}
	}

	return terminalRefreshRequest{
		Runtime: app.runtime, Workflows: app.workflows, SessionID: app.sessionID,
		TrackedAgentTaskIDs: agentIDs, TrackedWorkflowIDs: workflowIDs,
		KnownWorkflowIDs:  knownWorkflowIDs,
		LoadAgentPanel:    app.selectedPanelKind == panelAgentTasks,
		LoadWorkflowPanel: app.selectedPanelKind == panelWorkflows,
		WorkflowDetailID:  app.workflowPanelRunID,
	}
}

// loadTerminalRefreshSnapshot performs database reads only. It does not read or
// mutate App and is therefore safe to call from the refresh worker.
func newTerminalRefreshSnapshot(sessionID string) terminalRefreshSnapshot {
	var result terminalRefreshSnapshot

	result.SessionID = sessionID

	return result
}

func loadTerminalRefreshSnapshot(ctx context.Context, request *terminalRefreshRequest) terminalRefreshSnapshot {
	result := newTerminalRefreshSnapshot(request.SessionID)
	loadAgentRefreshSections(ctx, request, &result)
	loadWorkflowRefreshSections(ctx, request, &result)

	return result
}

func loadAgentRefreshSections(
	ctx context.Context,
	request *terminalRefreshRequest,
	result *terminalRefreshSnapshot,
) {
	if request.Runtime == nil || request.SessionID == "" {
		return
	}

	started := time.Now()

	limit := agentTaskInlineLimit
	if request.LoadAgentPanel {
		limit = agentTaskPanelLimit
	}

	listStarted := time.Now()

	tasks, err := request.Runtime.AgentTasks(ctx, request.SessionID, limit)
	if request.LoadAgentPanel {
		result.Timing.AgentPanel = time.Since(listStarted)
	}

	setAgentListSections(request, result, tasks, err)
	loadTrackedAgentTasks(ctx, request, result)
	result.Timing.AgentTasks = time.Since(started)

	toolStarted := time.Now()
	toolTasks, toolErr := request.Runtime.ToolTasks(ctx, request.SessionID, nil, toolTaskListLimit)
	result.ToolTasks = terminalRefreshSection[[]database.ToolTaskEntity]{
		Value: slices.Clone(toolTasks), Err: toolErr, Valid: toolErr == nil,
	}
	result.Timing.ToolTasks = time.Since(toolStarted)
}

func setAgentListSections(
	request *terminalRefreshRequest,
	result *terminalRefreshSnapshot,
	tasks []database.AgentTaskEntity,
	err error,
) {
	if err != nil {
		result.AgentTasks.Err = err
		result.AgentPanel.Err = err

		return
	}

	inline := tasks
	if len(inline) > agentTaskInlineLimit {
		inline = inline[:agentTaskInlineLimit]
	}

	result.AgentTasks = terminalRefreshSection[[]database.AgentTaskEntity]{
		Value: slices.Clone(inline), Err: nil, Valid: true,
	}
	if request.LoadAgentPanel {
		result.AgentPanel = terminalRefreshSection[[]database.AgentTaskEntity]{
			Value: slices.Clone(tasks), Err: nil, Valid: true,
		}
	}
}

func loadTrackedAgentTasks(
	ctx context.Context,
	request *terminalRefreshRequest,
	result *terminalRefreshSnapshot,
) {
	result.AgentTaskByID.Value = make(map[string]*database.AgentTaskEntity, len(request.TrackedAgentTaskIDs))
	result.AgentTaskByID.Valid = true

	listedTasks := result.AgentTasks.Value
	if result.AgentPanel.Valid {
		listedTasks = result.AgentPanel.Value
	}

	listed := make(map[string]*database.AgentTaskEntity, len(listedTasks))
	for index := range listedTasks {
		task := listedTasks[index]
		listed[task.Task.ID] = &task
	}

	for _, taskID := range request.TrackedAgentTaskIDs {
		if task, found := listed[taskID]; found {
			taskCopy := *task
			result.AgentTaskByID.Value[taskID] = &taskCopy

			continue
		}

		task, found, err := request.Runtime.AgentTask(ctx, taskID)
		if err != nil {
			result.AgentTaskByID.Err = err
			result.AgentTaskByID.Valid = false

			return
		}

		if found {
			taskCopy := *task
			result.AgentTaskByID.Value[taskID] = &taskCopy
		} else {
			result.AgentTaskByID.Value[taskID] = nil
		}
	}
}

func loadWorkflowRefreshSections(
	ctx context.Context,
	request *terminalRefreshRequest,
	result *terminalRefreshSnapshot,
) {
	if request.Workflows == nil || request.SessionID == "" {
		return
	}

	started := time.Now()
	runs, err := request.Workflows.ListActive(ctx, request.SessionID, agentTaskInlineLimit)
	result.ActiveWorkflow = terminalRefreshSection[[]database.WorkflowRunEntity]{
		Value: slices.Clone(runs), Err: err, Valid: err == nil,
	}
	loadWorkflowPanelSection(ctx, request, result)
	loadTrackedWorkflows(ctx, request, result, runs)
	runIDs := workflowDetailRunIDs(request, runs, result.WorkflowPanel.Value)
	result.Timing.Workflows = time.Since(started)

	detailsStarted := time.Now()
	details, detailsErr := loadWorkflowDetails(ctx, request.Workflows, runIDs)
	result.WorkflowDetail = terminalRefreshSection[workflowDetails]{
		Value: details, Err: detailsErr, Valid: detailsErr == nil,
	}
	result.Timing.Details = time.Since(detailsStarted)
}

func loadWorkflowPanelSection(
	ctx context.Context,
	request *terminalRefreshRequest,
	result *terminalRefreshSnapshot,
) {
	if !request.LoadWorkflowPanel {
		return
	}

	started := time.Now()
	runs, err := request.Workflows.List(ctx, request.SessionID, workflowPanelLimit)
	result.WorkflowPanel = terminalRefreshSection[[]database.WorkflowRunEntity]{
		Value: slices.Clone(runs), Err: err, Valid: err == nil,
	}
	result.Timing.WorkflowPanel = time.Since(started)
}

func loadTrackedWorkflows(
	ctx context.Context,
	request *terminalRefreshRequest,
	result *terminalRefreshSnapshot,
	runs []database.WorkflowRunEntity,
) {
	result.WorkflowByID.Value = make(map[string]*database.WorkflowRunEntity, len(request.TrackedWorkflowIDs)+1)
	result.WorkflowByID.Valid = true
	listed := workflowIDSet(runs, result.WorkflowPanel.Value)

	lookupIDs := slices.Clone(request.TrackedWorkflowIDs)
	if request.WorkflowDetailID != "" {
		lookupIDs = append(lookupIDs, request.WorkflowDetailID)
	}

	for _, runID := range lookupIDs {
		if _, found := listed[runID]; found {
			continue
		}

		run, found, err := request.Workflows.Get(ctx, runID)
		if err != nil {
			result.WorkflowByID.Err = err
			result.WorkflowByID.Valid = false

			return
		}

		if found {
			runCopy := *run
			result.WorkflowByID.Value[runID] = &runCopy
		} else {
			result.WorkflowByID.Value[runID] = nil
		}
	}
}

func workflowIDSet(groups ...[]database.WorkflowRunEntity) map[string]struct{} {
	result := make(map[string]struct{})

	for _, group := range groups {
		for index := range group {
			result[group[index].Task.ID] = struct{}{}
		}
	}

	return result
}

func workflowDetailRunIDs(
	request *terminalRefreshRequest,
	active []database.WorkflowRunEntity,
	panelRuns []database.WorkflowRunEntity,
) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(active)+len(panelRuns)+len(request.KnownWorkflowIDs)+1)
	appendID := func(id string) {
		if id != "" {
			if _, found := seen[id]; !found {
				seen[id] = struct{}{}
				result = append(result, id)
			}
		}
	}

	for _, id := range request.TrackedWorkflowIDs {
		appendID(id)
	}

	for _, id := range request.KnownWorkflowIDs {
		appendID(id)
	}

	for _, group := range [][]database.WorkflowRunEntity{active, panelRuns} {
		for index := range group {
			appendID(group[index].Task.ID)
		}
	}

	appendID(request.WorkflowDetailID)

	return result
}

func (snapshot *terminalRefreshSnapshot) hasErrors() bool {
	return snapshot.AgentTasks.Err != nil || snapshot.AgentPanel.Err != nil ||
		snapshot.AgentTaskByID.Err != nil || snapshot.ToolTasks.Err != nil ||
		snapshot.ActiveWorkflow.Err != nil || snapshot.WorkflowPanel.Err != nil ||
		snapshot.WorkflowByID.Err != nil || snapshot.WorkflowDetail.Err != nil
}

func loadWorkflowDetails(
	ctx context.Context,
	workflows workflowInspector,
	runIDs []string,
) (workflowDetails, error) {
	result := workflowDetails{
		ProgressByRun: make(map[string]workflowProgress, len(runIDs)),
		StepsByRun:    make(map[string][]database.WorkflowAgentTaskDetail, len(runIDs)),
	}
	for _, runID := range runIDs {
		result.ProgressByRun[runID] = workflowProgress{
			Total: 0, Succeeded: 0, Failed: 0, Running: 0,
		}
		result.StepsByRun[runID] = []database.WorkflowAgentTaskDetail{}
	}

	if len(runIDs) == 0 {
		return result, nil
	}

	details, err := workflows.AgentTaskDetails(ctx, slices.Clone(runIDs))
	if err != nil {
		return workflowDetails{}, terminalError(err, "list workflow agent task details")
	}

	for index := range details {
		detail := details[index]
		runID := detail.Link.WorkflowTaskID
		result.StepsByRun[runID] = append(result.StepsByRun[runID], detail)
		progress := result.ProgressByRun[runID]
		progress.Total++

		switch detail.AgentTask.Task.State {
		case database.TaskSucceeded:
			progress.Succeeded++
		case database.TaskFailed, database.TaskCanceled, database.TaskInterrupted:
			progress.Failed++
		case database.TaskQueued, database.TaskRunning, database.TaskCanceling:
			progress.Running++
		}

		result.ProgressByRun[runID] = progress
	}

	return result, nil
}

// applyTerminalRefreshSnapshot is UI-thread-only. Failed sections retain their
// previous values, including panel snapshots.
func (app *App) applyTerminalRefreshSnapshot(ctx context.Context, snapshot *terminalRefreshSnapshot) {
	if snapshot.SessionID != app.sessionID {
		return
	}

	if snapshot.AgentTasks.Valid {
		app.applyLoadedAgentTasks(ctx, snapshot.AgentTasks.Value, snapshot.AgentTaskByID)
	}

	if snapshot.ToolTasks.Valid {
		app.toolTasks = slices.Clone(snapshot.ToolTasks.Value)
	}

	if snapshot.ActiveWorkflow.Valid {
		app.applyLoadedWorkflows(ctx, snapshot)
	}

	app.reconcilePendingWorkflows(ctx, snapshot)

	if snapshot.AgentPanel.Valid {
		app.agentTaskPanelSnapshot = slices.Clone(snapshot.AgentPanel.Value)
		app.agentTaskPanelSnapshotValid = true
	}

	if snapshot.WorkflowPanel.Valid {
		app.workflowPanelSnapshot = slices.Clone(snapshot.WorkflowPanel.Value)
		app.workflowPanelSnapshotValid = true
	}

	app.applyWorkflowDetailSnapshot(ctx, snapshot)

	app.refreshAgentTasksPanelFromSnapshot()
	app.refreshWorkflowsPanelFromSnapshot()
	app.agentTasksRefreshedAt = time.Now()

	if snapshot.hasErrors() {
		app.setStatus("background task refresh failed; showing last known data")
	}
}

func (app *App) applyWorkflowDetailSnapshot(ctx context.Context, snapshot *terminalRefreshSnapshot) {
	if !snapshot.WorkflowDetail.Valid {
		return
	}

	if app.workflowProgress == nil {
		app.workflowProgress = map[string]workflowProgress{}
	}

	if app.workflowSteps == nil {
		app.workflowSteps = map[string][]database.WorkflowAgentTaskDetail{}
	}

	mergeWorkflowProgress(
		app.workflowProgress,
		snapshot.WorkflowDetail.Value.ProgressByRun,
		snapshot.ActiveWorkflow.Valid,
	)
	mergeWorkflowSteps(app.workflowSteps, snapshot.WorkflowDetail.Value.StepsByRun, snapshot.ActiveWorkflow.Valid)

	app.workflowDetailSnapshotValid = true
	app.hydrateAgentTaskUsageTotals()
	app.refreshWorkflowSummaryMetrics()

	if snapshot.ActiveWorkflow.Valid {
		app.watchActiveAgentTasks(ctx)
	}
}

func mergeWorkflowProgress(
	destination map[string]workflowProgress,
	source map[string]workflowProgress,
	replace bool,
) {
	for runID, progress := range source {
		if _, known := destination[runID]; replace || !known {
			destination[runID] = progress
		}
	}
}

func mergeWorkflowSteps(
	destination map[string][]database.WorkflowAgentTaskDetail,
	source map[string][]database.WorkflowAgentTaskDetail,
	replace bool,
) {
	for runID, steps := range source {
		if _, known := destination[runID]; replace || !known {
			destination[runID] = slices.Clone(steps)
		}
	}
}

func (app *App) applyLoadedAgentTasks(
	ctx context.Context,
	tasks []database.AgentTaskEntity,
	lookups terminalRefreshSection[map[string]*database.AgentTaskEntity],
) {
	listedByID := make(map[string]database.AgentTaskEntity, len(tasks))
	for index := range tasks {
		listedByID[tasks[index].Task.ID] = tasks[index]
	}

	activeByID := activeIndependentAgentTasksByID(tasks)
	active := make([]database.AgentTaskEntity, 0, len(activeByID)+len(app.agentTasks))
	completed := make([]database.AgentTaskEntity, 0)

	for index := range app.agentTasks {
		previous := app.agentTasks[index]

		current, complete := app.reconcileLoadedAgentTask(&previous, listedByID, activeByID, lookups)
		if current != nil {
			active = append(active, *current)
		}

		if complete != nil {
			completed = append(completed, *complete)
		}
	}

	for _, taskID := range slices.Sorted(maps.Keys(activeByID)) {
		active = append(active, activeByID[taskID])
	}

	app.agentTasks = active
	app.agentTaskSummaryOwnerID = app.sessionID
	app.hydrateAgentTaskUsageTotals()
	app.watchActiveAgentTasks(ctx)

	for index := range completed {
		app.deliverAgentTaskCompletion(ctx, &completed[index])
	}
}

func (app *App) reconcileLoadedAgentTask(
	previous *database.AgentTaskEntity,
	listed map[string]database.AgentTaskEntity,
	active map[string]database.AgentTaskEntity,
	lookups terminalRefreshSection[map[string]*database.AgentTaskEntity],
) (currentTask, completedTask *database.AgentTaskEntity) {
	if previous.Task.ParentTaskID != "" {
		app.stopAgentTaskWatch(previous.Task.ID)

		return nil, nil
	}

	if current, found := active[previous.Task.ID]; found {
		delete(active, previous.Task.ID)

		return &current, nil
	}

	if current, found := listed[previous.Task.ID]; found {
		if current.Task.ParentTaskID != "" {
			app.stopAgentTaskWatch(previous.Task.ID)

			return nil, nil
		}

		if isTerminalAgentTaskState(current.Task.State) {
			return nil, &current
		}

		return &current, nil
	}

	if !lookups.Valid {
		return previous, nil
	}

	latest, found := lookups.Value[previous.Task.ID]
	if !found {
		return previous, nil
	}

	if latest == nil {
		app.stopAgentTaskWatch(previous.Task.ID)

		return nil, nil
	}

	if isTerminalAgentTaskState(latest.Task.State) {
		return nil, latest
	}

	return latest, nil
}

func (app *App) applyLoadedWorkflows(ctx context.Context, snapshot *terminalRefreshSnapshot) {
	listed := make(map[string]database.WorkflowRunEntity, len(snapshot.ActiveWorkflow.Value))
	for index := range snapshot.ActiveWorkflow.Value {
		run := snapshot.ActiveWorkflow.Value[index]
		listed[run.Task.ID] = run
	}

	candidates := make([]database.WorkflowRunEntity, 0, len(listed)+len(app.activeWorkflows))
	for index := range app.activeWorkflows {
		current := app.reconcileLoadedWorkflow(ctx, &app.activeWorkflows[index], listed, snapshot.WorkflowByID)
		if current != nil {
			candidates = append(candidates, *current)
		}
	}

	for _, runID := range slices.Sorted(maps.Keys(listed)) {
		candidates = append(candidates, listed[runID])
	}

	app.activeWorkflows = candidates
	if snapshot.WorkflowDetail.Valid {
		app.activeWorkflows = visibleWorkflows(candidates, snapshot.WorkflowDetail.Value.StepsByRun)
	}

	if snapshot.WorkflowDetail.Valid && !app.hasActiveWorkflow(app.workflowSummaryRunID) {
		app.workflowSummaryRunID = ""
	}
}

func (app *App) reconcileLoadedWorkflow(
	ctx context.Context,
	previous *database.WorkflowRunEntity,
	listed map[string]database.WorkflowRunEntity,
	lookups terminalRefreshSection[map[string]*database.WorkflowRunEntity],
) *database.WorkflowRunEntity {
	if current, found := listed[previous.Task.ID]; found {
		delete(listed, previous.Task.ID)

		if isTerminalAgentTaskState(current.Task.State) {
			app.deliverWorkflowCompletion(ctx, &current)
		}

		return &current
	}

	if !lookups.Valid {
		return previous
	}

	latest, found := lookups.Value[previous.Task.ID]
	if !found {
		return previous
	}

	if latest != nil && isTerminalAgentTaskState(latest.Task.State) {
		app.deliverWorkflowCompletion(ctx, latest)
	}

	return latest
}

func visibleWorkflows(
	candidates []database.WorkflowRunEntity,
	stepsByRun map[string][]database.WorkflowAgentTaskDetail,
) []database.WorkflowRunEntity {
	active := make([]database.WorkflowRunEntity, 0, len(candidates))
	for index := range candidates {
		run := candidates[index]
		hasActiveChild := false

		for detailIndex := range stepsByRun[run.Task.ID] {
			if !isTerminalAgentTaskState(stepsByRun[run.Task.ID][detailIndex].AgentTask.Task.State) {
				hasActiveChild = true

				break
			}
		}

		if !isTerminalAgentTaskState(run.Task.State) || hasActiveChild {
			active = append(active, run)
		}
	}

	return active
}
