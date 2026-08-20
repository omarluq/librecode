package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	model "github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/transcript"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	agentDefaultDisplayName   = "agent"
	agentTaskPanelLimit       = 100
	agentTaskDescriptionLimit = 160
	agentTaskInlineLimit      = 20
	agentTaskRefreshInterval  = time.Second
	agentTaskLoadOperation    = "load agent task"
	noTaskResultMessage       = "No result was returned."
)

const (
	workflowToolName    = "workflow"
	agentStartToolName  = "agent_start"
	agentStatusToolName = "agent_status"
	agentWaitToolName   = "agent_wait"
	agentCancelToolName = "agent_cancel"
	agentListToolName   = "agent_list"
	taskToolPrefix      = "task_"
)

func isAgentManagementTool(name string) bool {
	switch name {
	case agentStartToolName, agentStatusToolName, agentWaitToolName, agentCancelToolName, agentListToolName:
		return true
	default:
		return false
	}
}

func isTaskManagementTool(name string) bool {
	return strings.HasPrefix(name, taskToolPrefix)
}

func (app *App) applyAgentToolEvent(ctx context.Context, event *assistant.ToolEvent) {
	if event == nil || event.IsError || app.runtime == nil || app.sessionID == "" {
		return
	}

	app.requestTerminalRefresh(ctx)
}

func (app *App) trackStartedAgentTask(ctx context.Context, event *assistant.ToolEvent) {
	taskID := agentTaskIDFromDetails(event.DetailsJSON)
	if taskID == "" {
		app.discoverActiveAgentTasks(ctx)

		return
	}

	for index := range app.agentTasks {
		if app.agentTasks[index].Task.ID == taskID {
			return
		}
	}

	task, found, err := app.runtime.AgentTask(ctx, taskID)
	if err != nil || !found {
		app.discoverActiveAgentTasks(ctx)

		return
	}

	if task.Task.ParentTaskID != "" {
		return
	}

	if isTerminalAgentTaskState(task.Task.State) {
		app.deliverAgentTaskCompletion(ctx, task)

		return
	}

	app.agentTasks = append(app.agentTasks, *task)
	app.agentTaskSummaryOwnerID = task.Task.OwnerSessionID
	app.watchActiveAgentTasks(ctx)
}

func agentTaskIDFromDetails(detailsJSON string) string {
	var details struct {
		TaskID string `json:"task_id"`
	}
	if json.Unmarshal([]byte(detailsJSON), &details) != nil {
		return ""
	}

	return strings.TrimSpace(details.TaskID)
}

func (app *App) resetAgentTaskTracking() {
	app.stopAgentTaskWatches()
	app.invalidateTerminalRefresh()
	app.agentTasks = nil
	app.toolTasks = nil
	app.activeWorkflows = nil
	app.agentTaskPanelSnapshot = nil
	app.workflowPanelSnapshot = nil
	app.agentTaskPanelSnapshotValid = false
	app.workflowPanelSnapshotValid = false
	app.workflowDetailSnapshotValid = false
	app.workflowProgress = map[string]workflowProgress{}
	app.pendingWorkflowRuns = map[string]int{}
	app.workflowSteps = map[string][]database.WorkflowAgentTaskDetail{}
	app.agentTaskUsageTotals = map[string]model.UsageTotals{}
	app.workflowSummaryMetrics = map[string]workflowSummaryMetric{}
	app.workflowSummaryRunID = ""
	app.workflowPanelRunID = ""
	app.agentTaskSummaryOwnerID = ""
	app.inspectedAgentTaskID = ""
	app.agentTasksRefreshedAt = time.Time{}
	app.deliveredAgentTasks = map[string]struct{}{}
}

func (app *App) refreshVisibleAgentTasks(ctx context.Context) {
	// Keep the parent's task summary stable while its child transcript is open.
	// Returning to the parent refreshes the retained summary.
	if len(app.agentTaskSessionStack) > 0 {
		return
	}

	if app.runtime == nil || app.sessionID == "" {
		app.agentTasks = nil
		app.activeWorkflows = nil
		app.agentTaskSummaryOwnerID = ""

		return
	}

	app.agentTaskSummaryOwnerID = app.sessionID

	if len(app.agentTasks) == 0 {
		app.discoverActiveAgentTasks(ctx)
	} else {
		app.refreshActiveAgentTasks(ctx)
	}

	app.refreshActiveWorkflows(ctx)

	app.agentTasksRefreshedAt = time.Now()
}

func (app *App) refreshActiveWorkflows(ctx context.Context) {
	if app.workflows == nil {
		app.activeWorkflows = nil

		return
	}

	runs, err := app.workflows.ListActive(ctx, app.sessionID, agentTaskInlineLimit)
	if err != nil {
		return
	}

	listed := make(map[string]database.WorkflowRunEntity, len(runs))
	for index := range runs {
		listed[runs[index].Task.ID] = runs[index]
	}

	candidates := app.reconcileTrackedWorkflows(ctx, listed, len(runs))
	for index := range runs {
		if run, found := listed[runs[index].Task.ID]; found {
			candidates = append(candidates, run)
		}
	}

	// Load direct children before filtering terminal runs. A workflow can finish
	// before children it launched, including before the first terminal refresh.
	app.activeWorkflows = candidates
	app.refreshWorkflowSummaryDetails(ctx)

	active := make([]database.WorkflowRunEntity, 0, len(candidates))
	for index := range candidates {
		run := candidates[index]
		if !isTerminalAgentTaskState(run.Task.State) || app.workflowHasActiveChildren(run.Task.ID) {
			active = append(active, run)
		}
	}

	app.activeWorkflows = active
	if !app.hasActiveWorkflow(app.workflowSummaryRunID) {
		app.workflowSummaryRunID = ""
	}
}

func (app *App) reconcileTrackedWorkflows(
	ctx context.Context,
	listed map[string]database.WorkflowRunEntity,
	capacity int,
) []database.WorkflowRunEntity {
	active := make([]database.WorkflowRunEntity, 0, capacity)

	for index := range app.activeWorkflows {
		previous := app.activeWorkflows[index]

		latest, keep := app.reconcileActiveWorkflow(ctx, &previous, listed)
		delete(listed, previous.Task.ID)

		if !keep {
			continue
		}

		if isTerminalAgentTaskState(latest.Task.State) {
			app.deliverWorkflowCompletion(ctx, &latest)
		}

		active = append(active, latest)
	}

	return active
}

func (app *App) workflowHasActiveChildren(runID string) bool {
	for index := range app.workflowSteps[runID] {
		if !isTerminalAgentTaskState(app.workflowSteps[runID][index].AgentTask.Task.State) {
			return true
		}
	}

	return false
}

func (app *App) hasActiveWorkflow(runID string) bool {
	if runID == "" {
		return false
	}

	for index := range app.activeWorkflows {
		if app.activeWorkflows[index].Task.ID == runID {
			return true
		}
	}

	return false
}

func (app *App) refreshWorkflowSummaryDetails(ctx context.Context) {
	runIDs := make([]string, len(app.activeWorkflows))
	for index := range app.activeWorkflows {
		runIDs[index] = app.activeWorkflows[index].Task.ID
	}

	details, err := app.loadWorkflowDetails(ctx, runIDs)
	if err != nil {
		return
	}

	app.workflowProgress = details.ProgressByRun
	app.workflowSteps = details.StepsByRun
	app.hydrateAgentTaskUsageTotals()
	app.refreshWorkflowSummaryMetrics()
	app.watchActiveAgentTasks(ctx)
}

func (app *App) reconcileActiveWorkflow(
	ctx context.Context,
	previous *database.WorkflowRunEntity,
	listed map[string]database.WorkflowRunEntity,
) (database.WorkflowRunEntity, bool) {
	if latest, found := listed[previous.Task.ID]; found {
		return latest, true
	}

	loaded, found, err := app.workflows.Get(ctx, previous.Task.ID)
	if err != nil {
		return *previous, true
	}

	if !found {
		return *previous, false
	}

	return *loaded, true
}

func (app *App) trackStartedWorkflow(ctx context.Context, event *assistant.ToolEvent) {
	runID := workflowRunIDFromDetails(event.DetailsJSON)
	if runID == "" || app.workflows == nil {
		app.refreshActiveWorkflows(ctx)

		return
	}

	for index := range app.activeWorkflows {
		if app.activeWorkflows[index].Task.ID == runID {
			return
		}
	}

	run, found, err := app.workflows.Get(ctx, runID)
	if err != nil || !found || run.Task.OwnerSessionID != app.sessionID {
		app.refreshActiveWorkflows(ctx)

		return
	}

	if isTerminalAgentTaskState(run.Task.State) {
		app.deliverWorkflowCompletion(ctx, run)

		return
	}

	app.activeWorkflows = append(app.activeWorkflows, *run)
}

func workflowRunIDFromDetails(detailsJSON string) string {
	var details struct {
		RunID string `json:"run_id"`
	}
	if json.Unmarshal([]byte(detailsJSON), &details) != nil {
		return ""
	}

	return strings.TrimSpace(details.RunID)
}

func (app *App) deliverWorkflowCompletion(ctx context.Context, run *database.WorkflowRunEntity) {
	if run == nil || (run.Task.State != database.TaskSucceeded && run.Task.State != database.TaskFailed) {
		return
	}

	runID := run.Task.ID
	if _, delivered := app.deliveredAgentTasks[runID]; delivered {
		return
	}

	app.deliveredAgentTasks[runID] = struct{}{}

	name := strings.TrimSpace(run.Name)
	if name == "" {
		name = toolDisplayWorkflow
	}

	detail := strings.TrimSpace(run.Task.Result)
	failed := run.Task.State == database.TaskFailed

	if failed {
		detail = strings.TrimSpace(run.Task.ErrorMessage)
		if detail == "" {
			detail = "No error detail was returned."
		}
	} else if detail == "" {
		detail = noTaskResultMessage
	}

	completion := fmt.Sprintf("Workflow %q (%s) finished with state %s.\n\n%s", name, runID, run.Task.State, detail)
	app.setStatus("workflow " + string(run.Task.State))

	content := formatToolEventForUI(&assistant.ToolEvent{
		CallID: "", ParentCallID: "", Sequence: 0, Name: "workflow_result",
		ArgumentsJSON: "", DetailsJSON: "", Result: completion, Error: run.Task.ErrorMessage, IsError: failed,
	})
	app.addAgentCompletionMessage(content)
	app.persistAgentCompletion(ctx, content)

	prompt := completion +
		"\n\nUse this completed workflow result to continue the current task and report the relevant findings."
	if failed {
		prompt = completion + "\n\nA background workflow failed after it was submitted. " +
			"Report the failure and relevant next step to the user."
	}

	app.deliverHiddenContinuation(ctx, prompt)
}

func (app *App) discoverActiveAgentTasks(ctx context.Context) {
	tasks, err := app.runtime.AgentTasks(ctx, app.sessionID, agentTaskInlineLimit)
	if err != nil {
		return
	}

	active := make([]database.AgentTaskEntity, 0, len(tasks))
	for index := range tasks {
		if tasks[index].Task.ParentTaskID != "" || isTerminalAgentTaskState(tasks[index].Task.State) {
			continue
		}

		active = append(active, tasks[index])
	}

	app.agentTasks = active
	app.agentTaskSummaryOwnerID = app.sessionID
	app.hydrateAgentTaskUsageTotals()
	app.watchActiveAgentTasks(ctx)
}

func (app *App) watchActiveAgentTasks(ctx context.Context) {
	desired := app.desiredAgentTaskWatches()

	for taskID := range app.agentTaskWatches {
		if _, keep := desired[taskID]; !keep {
			app.stopAgentTaskWatch(taskID)
		}
	}

	for taskID := range desired {
		if _, watching := app.agentTaskWatches[taskID]; watching {
			continue
		}

		events, cancelSubscription, err := app.runtime.SubscribeAgentTask(taskID)
		if err != nil {
			app.postAgentTaskWatchError(ctx, taskID, "failed to subscribe to agent task activity: "+err.Error())

			continue
		}

		watchCtx, cancelWatch := context.WithCancel(ctx)
		app.agentTaskWatches[taskID] = func() {
			cancelWatch()
			cancelSubscription()
		}

		go app.watchAgentTaskEventsWithRuntime(
			watchCtx, app.runtime, taskID, events, cancelSubscription, true,
		)
	}
}

func (app *App) watchAgentTask(
	ctx context.Context,
	taskID string,
	events <-chan database.TaskEventEntity,
	cancelSubscription func(),
) {
	app.watchAgentTaskEvents(ctx, taskID, events, cancelSubscription, false)
}

func (app *App) watchAgentTaskEvents(
	ctx context.Context,
	taskID string,
	events <-chan database.TaskEventEntity,
	cancelSubscription func(),
	replay bool,
) {
	app.watchAgentTaskEventsWithRuntime(ctx, app.runtime, taskID, events, cancelSubscription, replay)
}

func (app *App) watchAgentTaskEventsWithRuntime(
	ctx context.Context,
	runtime *assistant.Runtime,
	taskID string,
	events <-chan database.TaskEventEntity,
	cancelSubscription func(),
	replay bool,
) {
	defer cancelSubscription()

	var (
		sequence int64
		terminal bool
	)

	if replay {
		var err error

		sequence, terminal, err = app.replayAgentTaskEventsWithRuntime(ctx, runtime, taskID, 0)
		if err != nil {
			app.postAgentTaskReplayError(ctx, taskID, err)

			return
		}

		if terminal {
			return
		}
	}

	for {
		event, open := nextAgentTaskEvent(ctx, events)
		if !open {
			app.reconcileClosedAgentTaskWatch(ctx, runtime, taskID, sequence)

			return
		}

		sequence, terminal = app.forwardAgentTaskEventWithRuntime(
			ctx, runtime, taskID, &event, sequence, replay,
		)
		if terminal {
			return
		}
	}
}

func (app *App) reconcileClosedAgentTaskWatch(
	ctx context.Context,
	runtime *assistant.Runtime,
	taskID string,
	sequence int64,
) {
	if ctx.Err() != nil || runtime == nil {
		return
	}

	_, reachedTerminal, err := app.replayAgentTaskEventsWithRuntime(ctx, runtime, taskID, sequence)
	if err != nil {
		app.postAgentTaskReplayError(ctx, taskID, err)
	} else if !reachedTerminal {
		app.postAgentTaskWatchClosed(ctx, taskID)
	}
}

func nextAgentTaskEvent(
	ctx context.Context,
	events <-chan database.TaskEventEntity,
) (database.TaskEventEntity, bool) {
	select {
	case event, open := <-events:
		return event, open
	case <-ctx.Done():
		return database.TaskEventEntity{
			Event:  database.EventEntity{CreatedAt: time.Time{}, ID: "", Kind: "", PayloadJSON: ""},
			TaskID: "", Sequence: 0,
		}, false
	}
}

func (app *App) forwardAgentTaskEvent(
	ctx context.Context,
	taskID string,
	event *database.TaskEventEntity,
	sequence int64,
	replay bool,
) (int64, bool) {
	return app.forwardAgentTaskEventWithRuntime(ctx, app.runtime, taskID, event, sequence, replay)
}

func (app *App) forwardAgentTaskEventWithRuntime(
	ctx context.Context,
	runtime *assistant.Runtime,
	taskID string,
	event *database.TaskEventEntity,
	sequence int64,
	replay bool,
) (int64, bool) {
	if event.Sequence <= sequence {
		return sequence, false
	}

	if replay && event.Sequence > sequence+1 {
		var (
			terminal bool
			err      error
		)

		sequence, terminal, err = app.replayAgentTaskEventsWithRuntime(
			ctx, runtime, taskID, sequence,
		)
		if err != nil {
			app.postAgentTaskReplayError(ctx, taskID, err)

			return sequence, true
		}

		if terminal || event.Sequence <= sequence {
			return sequence, terminal
		}

		if event.Sequence != sequence+1 {
			app.postAgentTaskReplayError(ctx, taskID, fmt.Errorf(
				"durable replay ended at sequence %d before live sequence %d",
				sequence,
				event.Sequence,
			))

			return sequence, true
		}
	}

	sequence = event.Sequence
	if isTerminalAgentTaskEvent(event.Event.Kind) {
		app.postAgentTaskChanged(ctx, taskID)

		return sequence, true
	}

	app.postAgentTaskStreamEvent(ctx, event)

	return sequence, false
}

func (app *App) watchInspectedAgentTask(ctx context.Context, taskID string) {
	app.stopAgentTaskWatch(taskID)

	events, cancelSubscription, err := app.runtime.SubscribeAgentTask(taskID)
	if err != nil {
		app.postAgentTaskWatchError(ctx, taskID, "failed to subscribe to agent task activity: "+err.Error())

		return
	}

	watchCtx, cancelWatch := context.WithCancel(ctx)
	app.agentTaskWatches[taskID] = func() {
		cancelWatch()
		cancelSubscription()
	}

	go app.watchAgentTaskEventsWithRuntime(
		watchCtx, app.runtime, taskID, events, cancelSubscription, true,
	)
}

func (app *App) replayAgentTaskEventsWithRuntime(
	ctx context.Context,
	runtime *assistant.Runtime,
	taskID string,
	after int64,
) (sequence int64, terminal bool, err error) {
	const replayLimit = 256

	for {
		events, err := runtime.AgentTaskEvents(ctx, taskID, after, replayLimit)
		if err != nil {
			return after, false, fmt.Errorf("replay agent task events: %w", err)
		}

		var terminal bool

		after, terminal, err = app.applyReplayedAgentTaskEvents(ctx, taskID, events, after)
		if err != nil || terminal {
			return after, terminal, err
		}

		if len(events) < replayLimit {
			return after, false, nil
		}
	}
}

func (app *App) applyReplayedAgentTaskEvents(
	ctx context.Context,
	taskID string,
	events []database.TaskEventEntity,
	after int64,
) (sequence int64, terminal bool, err error) {
	for index := range events {
		event := &events[index]
		if event.Sequence <= after {
			continue
		}

		if event.Sequence != after+1 {
			return after, false, fmt.Errorf(
				"replay agent task events: expected sequence %d, got %d",
				after+1,
				event.Sequence,
			)
		}

		after = event.Sequence
		if isTerminalAgentTaskEvent(event.Event.Kind) {
			app.postAgentTaskChanged(ctx, taskID)

			return after, true, nil
		}

		app.postAgentTaskStreamEvent(ctx, event)
	}

	return after, false, nil
}

func (app *App) postAgentTaskChanged(ctx context.Context, taskID string) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventAgentTaskChanged, Provider: "", Text: taskID, PromptID: 0,
	})
}

func (app *App) postAgentTaskWatchClosed(ctx context.Context, taskID string) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventAgentTaskReplayError, Provider: taskID,
		Text: "agent task event stream closed; refreshed durable state", PromptID: 0,
	})
}

func (app *App) postAgentTaskReplayError(ctx context.Context, taskID string, err error) {
	app.postAgentTaskWatchError(ctx, taskID, "failed to replay agent task activity: "+err.Error())
}

func (app *App) postAgentTaskWatchError(ctx context.Context, taskID, message string) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventAgentTaskReplayError, Provider: taskID, Text: message, PromptID: 0,
	})
}

func (app *App) handleAgentTaskWatchError(_ context.Context, taskID, message string) {
	app.addSystemMessage(message)

	// Leave the failed watch stopped. The next periodic snapshot can retry it
	// without recursively publishing another event from this handler.
	app.stopAgentTaskWatch(taskID)
}

func (app *App) postAgentTaskStreamEvent(ctx context.Context, event *database.TaskEventEntity) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventAgentTaskStream, Provider: event.TaskID, Text: event.Event.PayloadJSON, PromptID: 0,
	})
}

func (app *App) handleAgentTaskTerminalEvent(ctx context.Context, taskID string) {
	if len(app.agentTaskSessionStack) > 0 {
		task, found, err := app.runtime.AgentTask(ctx, taskID)
		if err == nil && found && app.inspectingWhilePromptRuns() {
			app.deliverAgentTaskCompletion(ctx, task)
		}

		app.refreshInspectedParentAgentTask(ctx, taskID)
		app.reloadInspectedAgentTaskTranscript(ctx, taskID)

		return
	}

	// Durable terminal state and completion data are reconciled by the coalesced
	// worker. Never re-enter the repository from the normal summary event path.
	app.requestTerminalRefresh(ctx)
}

func (app *App) applyInspectedAgentTaskEvent(_ context.Context, taskID, payloadJSON string) {
	if taskID == "" {
		return
	}

	app.recordAgentTaskUsageTotals(taskID, payloadJSON)

	if len(app.agentTaskSessionStack) == 0 {
		return
	}

	var streamEvent assistant.StreamEvent
	if err := json.Unmarshal([]byte(payloadJSON), &streamEvent); err != nil {
		return
	}

	payload, ok := asyncEventFromStreamEvent(streamEvent, 0)
	if !ok {
		return
	}

	app.renderInspectedAgentTaskEvent(payload)
}

func (app *App) recordAgentTaskUsageTotals(taskID, payloadJSON string) {
	var usageTotals model.UsageTotals
	if json.Unmarshal([]byte(payloadJSON), &usageTotals) != nil || !usageTotals.Reported {
		return
	}

	if app.agentTaskUsageTotals == nil {
		app.agentTaskUsageTotals = make(map[string]model.UsageTotals)
	}

	previous, found := app.agentTaskUsageTotals[taskID]

	regressed := found && (usageTotals.InputTokens < previous.InputTokens ||
		usageTotals.OutputTokens < previous.OutputTokens ||
		usageTotals.ProviderRoundTrips < previous.ProviderRoundTrips)
	if regressed {
		return
	}

	app.agentTaskUsageTotals[taskID] = usageTotals
	app.refreshWorkflowSummaryMetricsForTask(taskID)
}

func (app *App) renderInspectedAgentTaskEvent(payload *asyncEvent) {
	if payload == nil {
		return
	}

	switch payload.Kind {
	case asyncEventPromptDelta:
		app.appendStreamingBlock(transcript.RoleAssistant, payload.Text)
	case asyncEventPromptThinkingDelta:
		app.appendStreamingBlock(transcript.RoleThinking, payload.Text)
	case asyncEventPromptToolStart:
		app.applyStreamedToolStart(payload.ToolCallEvent, payload.Text)
	case asyncEventPromptToolResult:
		app.renderInspectedToolResult(payload.ToolEvent)
	case asyncEventPromptContext,
		asyncEventCompactStart,
		asyncEventCompactDone,
		asyncEventCompactError:
		if payload.Text != "" {
			app.addSystemMessage(payload.Text)
		}
	case asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot,
		asyncEventPromptDone,
		asyncEventSteeringReturn,
		asyncEventSteeringConsumed,
		asyncEventPromptUserEntry,
		asyncEventPromptRetry,
		asyncEventPromptError,
		asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventAgentTaskChanged,
		asyncEventAgentTaskStream,
		asyncEventAgentTaskReplayError,
		asyncEventAgentTaskCompleted:
		return
	}
}

func (app *App) renderInspectedToolResult(event *assistant.ToolEvent) {
	if event == nil || isAgentManagementTool(event.Name) {
		return
	}

	app.removeRunningToolBlock(event)
	app.appendStreamingBlock(transcript.RoleToolResult, formatToolEventForUI(event))
	app.streamedToolEvents++
}

func (app *App) reloadInspectedAgentTaskTranscript(ctx context.Context, taskID string) {
	task, found, err := app.runtime.AgentTask(ctx, taskID)
	if err != nil || !found || task.ChildSessionID != app.sessionID {
		return
	}

	messages, err := app.sessionMessages(ctx, task.ChildSessionID)
	if err != nil {
		app.addSystemMessage(err.Error())

		return
	}

	app.resetMessages()
	app.resetStreamingBlocks()
	app.appendSessionMessages(messages)
	app.addSystemMessage("inspecting agent task: " + taskID + "; select main above to return")
}

func (app *App) refreshInspectedParentAgentTask(ctx context.Context, taskID string) {
	if app.runtime == nil || taskID == "" {
		return
	}

	latest, found, err := app.runtime.AgentTask(ctx, taskID)
	if err != nil || !found {
		return
	}

	for index := range app.agentTasks {
		if app.agentTasks[index].Task.ID != taskID {
			continue
		}

		app.agentTasks[index] = *latest
		if isTerminalAgentTaskState(latest.Task.State) {
			app.stopAgentTaskWatch(taskID)
		}

		return
	}
}

const (
	agentTaskSucceededEvent = "task_succeeded"
	taskQueuedLabel         = "queued"
)

func isTerminalAgentTaskEvent(kind string) bool {
	switch kind {
	case agentTaskSucceededEvent, "task_failed", "task_canceled", "task_interrupted":
		return true
	default:
		return false
	}
}

func (app *App) desiredAgentTaskWatches() map[string]struct{} {
	desired := make(map[string]struct{})

	for index := range app.agentTasks {
		task := &app.agentTasks[index].Task
		if !isTerminalAgentTaskState(task.State) {
			desired[task.ID] = struct{}{}
		}
	}

	for _, steps := range app.workflowSteps {
		for index := range steps {
			task := &steps[index].AgentTask.Task
			if !isTerminalAgentTaskState(task.State) {
				desired[task.ID] = struct{}{}
			}
		}
	}

	if app.inspectedAgentTaskID != "" {
		desired[app.inspectedAgentTaskID] = struct{}{}
	}

	return desired
}

func (app *App) stopAgentTaskWatch(taskID string) {
	cancel, watching := app.agentTaskWatches[taskID]
	if !watching {
		return
	}

	delete(app.agentTaskWatches, taskID)
	cancel()
}

func (app *App) stopAgentTaskWatches() {
	for taskID := range app.agentTaskWatches {
		app.stopAgentTaskWatch(taskID)
	}
}

func (app *App) refreshActiveAgentTasks(ctx context.Context) {
	tasks, err := app.runtime.AgentTasks(ctx, app.sessionID, agentTaskInlineLimit)
	if err != nil {
		return
	}

	activeByID := activeIndependentAgentTasksByID(tasks)

	active := make([]database.AgentTaskEntity, 0, len(activeByID))
	completed := make([]database.AgentTaskEntity, 0)

	for index := range app.agentTasks {
		previous := app.agentTasks[index]
		if previous.Task.ParentTaskID != "" {
			app.stopAgentTaskWatch(previous.Task.ID)

			continue
		}

		task, found := activeByID[previous.Task.ID]
		if !found {
			retained, finished := app.reconcileMissingAgentTask(ctx, &previous)
			if retained != nil {
				active = append(active, *retained)
			}

			if finished != nil {
				completed = append(completed, *finished)
			}

			continue
		}

		active = append(active, task)

		delete(activeByID, task.Task.ID)
	}

	for taskID := range activeByID {
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

func activeIndependentAgentTasksByID(
	tasks []database.AgentTaskEntity,
) map[string]database.AgentTaskEntity {
	activeByID := make(map[string]database.AgentTaskEntity, len(tasks))
	for index := range tasks {
		task := tasks[index]
		if task.Task.ParentTaskID == "" && !isTerminalAgentTaskState(task.Task.State) {
			activeByID[task.Task.ID] = task
		}
	}

	return activeByID
}

func (app *App) reconcileMissingAgentTask(
	ctx context.Context,
	previous *database.AgentTaskEntity,
) (retained, completed *database.AgentTaskEntity) {
	latest, found, err := app.runtime.AgentTask(ctx, previous.Task.ID)
	if err != nil {
		// Keep the last snapshot when reconciliation fails transiently.
		return previous, nil
	}

	if !found {
		app.stopAgentTaskWatch(previous.Task.ID)

		return nil, nil
	}

	if isTerminalAgentTaskState(latest.Task.State) {
		return nil, latest
	}

	// The bounded list can omit older active tasks.
	return latest, nil
}

func agentTaskCompletion(
	previous database.TaskState,
	task *database.AgentTaskEntity,
) (string, bool) {
	if isTerminalAgentTaskState(previous) || !isTerminalAgentTaskState(task.Task.State) {
		return "", false
	}

	result := task.Task.Result
	if result == "" {
		result = task.Task.ErrorMessage
	}

	if result == "" {
		result = noTaskResultMessage
	}

	return fmt.Sprintf(
		"Agent %s (%s) finished with state %s.\n\n%s",
		task.AgentName,
		task.Task.ID,
		task.Task.State,
		result,
	), true
}

func (app *App) deliverAgentTaskCompletion(ctx context.Context, task *database.AgentTaskEntity) {
	if task == nil {
		return
	}

	if task.Task.ParentTaskID != "" {
		app.discardAgentTaskCompletion(task.Task.ID)

		return
	}

	completion, completed := agentTaskCompletion(database.TaskQueued, task)
	if !completed {
		return
	}

	if !app.withSessionView(task.Task.OwnerSessionID, func() {
		app.deliverKnownAgentTaskCompletionText(ctx, task.Task.ID, completion)
	}) {
		app.setStatus("agent result owner view is unavailable")
	}
}

func (app *App) deliverAgentTaskCompletionEvent(ctx context.Context, taskID, completion string) {
	ownerSessionID := app.sessionID
	if app.runtime != nil {
		task, found, err := app.runtime.AgentTask(ctx, taskID)
		if err != nil || !found || task.Task.OwnerSessionID == "" {
			app.setStatus("agent result owner could not be resolved")

			return
		}

		ownerSessionID = task.Task.OwnerSessionID
	}

	if !app.withSessionView(ownerSessionID, func() {
		app.deliverAgentTaskCompletionText(ctx, taskID, completion)
	}) {
		app.setStatus("agent result owner view is unavailable")
	}
}

func (app *App) deliverAgentTaskCompletionText(ctx context.Context, taskID, completion string) {
	if taskID == "" || completion == "" {
		return
	}

	if _, delivered := app.deliveredAgentTasks[taskID]; delivered {
		return
	}

	workflowChild := app.isTrackedWorkflowChild(taskID) || app.isPersistedWorkflowChild(ctx, taskID)
	app.finishAgentTaskCompletion(ctx, taskID, completion, workflowChild)
}

func (app *App) deliverKnownAgentTaskCompletionText(ctx context.Context, taskID, completion string) {
	app.finishAgentTaskCompletion(ctx, taskID, completion, app.isTrackedWorkflowChild(taskID))
}

func (app *App) finishAgentTaskCompletion(ctx context.Context, taskID, completion string, workflowChild bool) {
	if taskID == "" || completion == "" {
		return
	}

	if _, delivered := app.deliveredAgentTasks[taskID]; delivered {
		return
	}

	app.discardAgentTaskCompletion(taskID)

	if !workflowChild {
		app.deliverAgentTaskCompletions(ctx, []string{completion})
	}
}

func (app *App) isTrackedWorkflowChild(taskID string) bool {
	for index := range app.agentTasks {
		if app.agentTasks[index].Task.ID == taskID && app.agentTasks[index].Task.ParentTaskID != "" {
			return true
		}
	}

	for _, details := range app.workflowSteps {
		for index := range details {
			if details[index].Link.AgentTaskID == taskID {
				return true
			}
		}
	}

	return false
}

func (app *App) isPersistedWorkflowChild(ctx context.Context, taskID string) bool {
	if app.runtime == nil {
		return false
	}

	task, found, err := app.runtime.AgentTask(ctx, taskID)

	return err == nil && found && task.Task.ParentTaskID != ""
}

func (app *App) discardAgentTaskCompletion(taskID string) {
	app.deliveredAgentTasks[taskID] = struct{}{}
	app.stopAgentTaskWatch(taskID)

	active := app.agentTasks[:0]
	for index := range app.agentTasks {
		if app.agentTasks[index].Task.ID != taskID {
			active = append(active, app.agentTasks[index])
		}
	}

	app.agentTasks = active
}

func (app *App) deliverAgentTaskCompletions(ctx context.Context, completions []string) {
	if len(completions) == 0 {
		return
	}

	app.setStatus(fmt.Sprintf("%d agent task(s) finished", len(completions)))

	for _, completion := range completions {
		content := formatAgentCompletionForUI(completion)
		app.addAgentCompletionMessage(content)
		app.persistAgentCompletion(ctx, content)
	}

	prompt := strings.Join(completions, "\n\n---\n\n") +
		"\n\nUse these completed subagent results to continue the current task and report the relevant findings."
	app.deliverHiddenContinuation(ctx, prompt)
}

func (app *App) addAgentCompletionMessage(content string) {
	message := newChatMessage(transcript.RoleToolResult, content)
	app.liveAgentCompletions = append(app.liveAgentCompletions, message)
}

func (app *App) commitLiveAgentCompletions() {
	// Keep completions in the dynamic tail while their hidden continuation is
	// queued. Moving them into history sooner lets the next streaming response
	// reserve the viewport and hide the result that triggered it.
	if len(app.hiddenQueuedMessages) > 0 {
		return
	}

	for _, message := range app.liveAgentCompletions {
		app.appendMessage(message)
	}

	app.liveAgentCompletions = app.liveAgentCompletions[:0]
}

func (app *App) persistAgentCompletion(ctx context.Context, content string) {
	if app.runtime == nil || app.sessionID == "" {
		return
	}

	modelFacing := false

	_, err := app.runtime.SessionRepository().AppendMessageWithModelFacing(
		context.WithoutCancel(ctx),
		app.sessionID,
		nil,
		&database.MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      database.RoleToolResult,
			Content:   content,
			Provider:  "",
			Model:     "", Parts: nil,
		},
		&modelFacing,
	)
	if err != nil {
		app.setStatus("agent result could not be saved")
	}
}

func formatAgentCompletionForUI(completion string) string {
	return formatToolEventForUI(&assistant.ToolEvent{
		CallID:        "",
		ParentCallID:  "",
		Sequence:      0,
		Name:          "agent_result",
		ArgumentsJSON: "",
		DetailsJSON:   "",
		Result:        completion,
		Error:         "",
		IsError:       false,
	})
}

func (app *App) hasRunningAgentTasks() bool {
	for index := range app.activeWorkflows {
		run := &app.activeWorkflows[index]
		if !isTerminalAgentTaskState(run.Task.State) || app.workflowHasActiveChildren(run.Task.ID) {
			return true
		}
	}

	for index := range app.agentTasks {
		if app.agentTasks[index].Task.ParentTaskID == "" &&
			!isTerminalAgentTaskState(app.agentTasks[index].Task.State) {
			return true
		}
	}

	return false
}

func isTerminalAgentTaskState(state database.TaskState) bool {
	switch state {
	case database.TaskSucceeded, database.TaskFailed, database.TaskCanceled, database.TaskInterrupted:
		return true
	case database.TaskQueued, database.TaskRunning, database.TaskCanceling:
		return false
	default:
		return false
	}
}

const (
	workflowStepMinimumWidth = 8
	workflowStatusWidth      = 12
	workflowTableFixedWidth  = 24
	workflowDetailFixedRows  = 3
)

func (app *App) renderAgentTaskSummary(width int) []tui.Line {
	if len(app.agentTasks) == 0 && len(app.activeWorkflows) == 0 {
		return nil
	}

	indicatorStyle := tcell.StyleDefault.Foreground(defaultWorkingShimmerBrightColor()).Bold(true)
	labelStyle := tcell.StyleDefault.Foreground(app.theme.colors[colorMuted])
	headerStyle := tcell.StyleDefault.Foreground(app.theme.colors[colorDim]).Bold(true)

	lines := make(
		[]tui.Line,
		0,
		len(app.activeWorkflows)+len(app.agentTasks)+agentTaskSummaryContentStart+1,
	)
	selectedIndex := app.selectedAgentTaskSummaryIndex()
	mainLine := app.renderMainAgentTaskSummaryLine(
		width, labelStyle, indicatorStyle, selectedIndex == agentTaskSummaryMainIndex,
	)
	lines = append(lines, mainLine)
	selectableIndex := agentTaskSummaryContentStart

	if run := app.expandedWorkflowSummaryRun(); run != nil {
		lines = append(
			lines,
			app.renderWorkflowSummaryDetail(
				run, width, labelStyle, headerStyle, selectedIndex == agentTaskSummaryContentStart,
			)...,
		)

		return padAgentTaskSummary(lines, app.agentTaskSummaryHeight())
	}

	for index := range app.activeWorkflows {
		run := &app.activeWorkflows[index]
		label := app.workflowSummaryLabelForWidth(run, max(0, width-tui.Width(pendingToolIndicator+" ")))
		line := tui.Line{
			Text:  pendingToolIndicator + " " + label,
			Style: labelStyle,
			Spans: []tui.Span{
				{Text: pendingToolIndicator, Style: indicatorStyle},
				{Text: " " + label, Style: labelStyle},
			},
		}
		lines = append(
			lines,
			app.styleAgentTaskSummaryLine(line, width, selectableIndex == selectedIndex),
		)
		selectableIndex++
	}

	for index := range app.agentTasks {
		task := &app.agentTasks[index]
		if task.Task.ParentTaskID != "" {
			continue
		}

		label := app.agentTaskSummaryLabelForWidth(task, time.Now(), max(0, width-tui.Width(pendingToolIndicator+" ")))
		line := tui.Line{
			Text:  pendingToolIndicator + " " + label,
			Style: labelStyle,
			Spans: []tui.Span{
				{Text: pendingToolIndicator, Style: indicatorStyle},
				{Text: " " + label, Style: labelStyle},
			},
		}
		lines = append(
			lines,
			app.styleAgentTaskSummaryLine(line, width, selectableIndex == selectedIndex),
		)
		selectableIndex++
	}

	if len(lines) == 0 {
		return nil
	}

	lines = append(lines, tui.NewLine(tcell.StyleDefault, ""))

	return padAgentTaskSummary(lines, app.agentTaskSummaryHeight())
}

func (app *App) renderMainAgentTaskSummaryLine(
	width int,
	labelStyle tcell.Style,
	indicatorStyle tcell.Style,
	selected bool,
) tui.Line {
	return app.styleAgentTaskSummaryLine(tui.Line{
		Text:  pendingToolIndicator + " main",
		Style: labelStyle,
		Spans: []tui.Span{
			{Text: pendingToolIndicator, Style: indicatorStyle},
			{Text: " main", Style: labelStyle},
		},
	}, width, selected)
}

func (app *App) agentTaskSummaryHeight() int {
	collapsedHeight := len(app.activeWorkflows) + agentTaskSummaryContentStart + 1
	for index := range app.agentTasks {
		if app.agentTasks[index].Task.ParentTaskID == "" {
			collapsedHeight++
		}
	}

	if app.workflowSummaryRunID == "" {
		return collapsedHeight
	}

	reservedHeight := collapsedHeight

	for index := range app.activeWorkflows {
		stepRows := max(1, len(app.workflowSteps[app.activeWorkflows[index].Task.ID]))
		reservedHeight = max(
			reservedHeight,
			agentTaskSummaryContentStart+stepRows+workflowDetailFixedRows,
		)
	}

	return reservedHeight
}

func padAgentTaskSummary(lines []tui.Line, height int) []tui.Line {
	padding := height - len(lines)
	if padding <= 0 {
		return lines
	}

	for range padding {
		lines = append(lines, tui.NewLine(tcell.StyleDefault, ""))
	}

	return lines
}

func (app *App) expandedWorkflowSummaryRun() *database.WorkflowRunEntity {
	for index := range app.activeWorkflows {
		if app.activeWorkflows[index].Task.ID == app.workflowSummaryRunID {
			return &app.activeWorkflows[index]
		}
	}

	return nil
}

func (app *App) renderWorkflowSummaryDetail(
	run *database.WorkflowRunEntity,
	width int,
	labelStyle tcell.Style,
	headerStyle tcell.Style,
	selected bool,
) []tui.Line {
	lines := make([]tui.Line, 0, len(app.workflowSteps[run.Task.ID])+workflowDetailFixedRows)
	heading := tui.NewLine(labelStyle, "Workflow: "+app.workflowSummaryLabel(run))
	lines = append(lines, app.styleAgentTaskSummaryLine(heading, width, selected))

	stepWidth := max(workflowStepMinimumWidth, width-workflowTableFixedWidth)
	header := tui.PadRight("STEP", stepWidth) + "  " +
		tui.PadRight("STATUS", workflowStatusWidth) + "  ELAPSED"
	lines = append(lines, tui.NewLine(headerStyle, tui.Truncate(header, width)))

	steps := app.workflowSteps[run.Task.ID]
	if len(steps) == 0 {
		row := workflowStepRow("workflow", run.Task.State, taskMeta(&run.Task, time.Now()), stepWidth)
		lines = append(lines, tui.NewLine(labelStyle, tui.Truncate(row, width)))
	} else {
		for index := range steps {
			detail := &steps[index]
			row := workflowStepRow(
				workflowStepName(&detail.Link),
				detail.AgentTask.Task.State,
				taskMeta(&detail.AgentTask.Task, time.Now()),
				stepWidth,
			)
			lines = append(lines, tui.NewLine(labelStyle, tui.Truncate(row, width)))
		}
	}

	return append(lines, tui.NewLine(tcell.StyleDefault, ""))
}

func (app *App) selectedAgentTaskSummaryIndex() int {
	if app.validateAgentTaskSummarySelection() {
		return app.agentTaskSummarySelection.ItemIndex
	}

	return -1
}

func (app *App) styleAgentTaskSummaryLine(line tui.Line, width int, selected bool) tui.Line {
	if width <= 0 {
		return tui.Line{Text: "", Style: line.Style, Spans: []tui.Span{}}
	}

	line = line.Truncate(width)
	if selected {
		return applyLineStyle(line, app.theme.selected())
	}

	return line
}

func workflowStepName(link *database.WorkflowAgentTaskEntity) string {
	name := strings.TrimSpace(link.NodeKey)
	if name == "" {
		name = "agent"
	}

	return fmt.Sprintf("%s[%d]", name, link.InvocationIndex)
}

func workflowStepRow(name string, state database.TaskState, elapsed string, stepWidth int) string {
	return tui.PadRight(tui.Truncate(name, stepWidth), stepWidth) + "  " +
		tui.PadRight(string(state), workflowStatusWidth) + "  " + elapsed
}

func (app *App) workflowSummaryLabel(run *database.WorkflowRunEntity) string {
	return app.workflowSummaryLabelForWidth(run, math.MaxInt)
}

func (app *App) workflowSummaryLabelForWidth(run *database.WorkflowRunEntity, width int) string {
	if run == nil {
		return toolDisplayWorkflow
	}

	metrics := app.workflowSummaryMetrics[run.Task.ID]
	terminal, total, usageTotals := metrics.terminal, metrics.total, metrics.usage

	if total == 0 {
		progress := app.workflowProgress[run.Task.ID]
		terminal, total = progress.Succeeded+progress.Failed, progress.Total
	}

	identitySuffix := fmt.Sprintf(") %d/%d agents", terminal, total)
	elapsed := summaryElapsed(&run.Task, time.Now())
	token := usageTotalsSuffix(usageTotals, total)

	return fitSummaryIdentity(toolDisplayWorkflow+"(", workflowName(run), identitySuffix, elapsed, token, width)
}

type workflowSummaryMetric struct {
	usage    model.UsageAggregate
	terminal int
	total    int
}

func (app *App) refreshWorkflowSummaryMetrics() {
	app.workflowSummaryMetrics = make(map[string]workflowSummaryMetric, len(app.workflowSteps))
	for runID, details := range app.workflowSteps {
		app.workflowSummaryMetrics[runID] = newWorkflowSummaryMetric(details, app.agentTaskUsageTotals)
	}
}

func (app *App) refreshWorkflowSummaryMetricsForTask(taskID string) {
	if app.workflowSummaryMetrics == nil {
		app.workflowSummaryMetrics = make(map[string]workflowSummaryMetric)
	}

	for runID, details := range app.workflowSteps {
		for index := range details {
			if details[index].AgentTask.Task.ID == taskID {
				app.workflowSummaryMetrics[runID] = newWorkflowSummaryMetric(details, app.agentTaskUsageTotals)

				break
			}
		}
	}
}

func newWorkflowSummaryMetric(
	details []database.WorkflowAgentTaskDetail,
	live map[string]model.UsageTotals,
) workflowSummaryMetric {
	terminal, total, usage := workflowSummaryMetricsWithLive(details, live)

	return workflowSummaryMetric{usage: usage, terminal: terminal, total: total}
}

func workflowSummaryMetricsWithLive(
	details []database.WorkflowAgentTaskDetail,
	live map[string]model.UsageTotals,
) (terminal, total int, aggregate model.UsageAggregate) {
	seen := make(map[string]struct{}, len(details))
	allUsage := make([]model.UsageTotals, 0, len(details))
	terminal = 0

	for index := range details {
		detail := &details[index]

		taskID := detail.AgentTask.Task.ID
		if _, ok := seen[taskID]; ok {
			continue
		}

		seen[taskID] = struct{}{}

		if isTerminalAgentTaskState(detail.AgentTask.Task.State) {
			terminal++
		}

		usage, ok := decodeFinalUsageTotals(detail.AgentTask.UsageJSON)
		if !ok && !isTerminalAgentTaskState(detail.AgentTask.Task.State) {
			usage, ok = live[taskID]
		}

		if !ok {
			usage = model.UsageTotals{
				InputTokens: 0, OutputTokens: 0, ProviderRoundTrips: 0, Reported: false,
			}
		}

		allUsage = append(allUsage, usage)
	}

	aggregate, err := model.AggregateUsage(allUsage)
	if err != nil {
		return terminal, len(seen), model.UsageAggregate{
			Usage: model.UsageTotals{
				InputTokens: 0, OutputTokens: 0, ProviderRoundTrips: 0, Reported: false,
			},
			Known: 0, Total: len(seen),
		}
	}

	return terminal, len(seen), aggregate
}

func agentTaskSummaryLabel(task *database.AgentTaskEntity) string {
	if task == nil {
		return agentDefaultDisplayName
	}

	name := strings.TrimSpace(task.AgentName)
	if name == "" {
		name = agentDefaultDisplayName
	}

	prompt := strings.Join(strings.Fields(task.Prompt), " ")
	if prompt == "" {
		return name
	}

	return name + "(" + prompt + ")"
}

func agentTaskSummaryLabelForWidth(task *database.AgentTaskEntity, now time.Time, width int) string {
	if task == nil {
		return agentDefaultDisplayName
	}

	usageTotals, known := decodeFinalUsageTotals(task.UsageJSON)

	return agentTaskSummaryLabelWithUsageTotals(task, now, width, usageTotals, known)
}

func (app *App) agentTaskSummaryLabelForWidth(task *database.AgentTaskEntity, now time.Time, width int) string {
	usageTotals, known := app.agentTaskUsageTotals[task.Task.ID]
	if final, ok := decodeFinalUsageTotals(task.UsageJSON); ok {
		usageTotals, known = final, true
	} else if isTerminalAgentTaskState(task.Task.State) {
		usageTotals, known = emptyUsageTotals(), false
	}

	return agentTaskSummaryLabelWithUsageTotals(task, now, width, usageTotals, known)
}

func agentTaskSummaryLabelWithUsageTotals(
	task *database.AgentTaskEntity,
	now time.Time,
	width int,
	usageTotals model.UsageTotals,
	known bool,
) string {
	if task == nil {
		return agentDefaultDisplayName
	}

	name := strings.TrimSpace(task.AgentName)
	if name == "" {
		name = agentDefaultDisplayName
	}

	prompt := strings.Join(strings.Fields(task.Prompt), " ")
	elapsed := summaryElapsed(&task.Task, now)
	token := ""

	if task.Task.StartedAt != nil {
		if known {
			total, err := usageTotals.TotalTokens()
			if err == nil {
				token = compactCount64(total) + " tok"
			}
		}
	}

	return fitSummaryIdentity(name+"(", prompt, ")", elapsed, token, width)
}

func (app *App) hydrateAgentTaskUsageTotals() {
	if app.agentTaskUsageTotals == nil {
		app.agentTaskUsageTotals = make(map[string]model.UsageTotals)
	}

	for index := range app.agentTasks {
		app.hydrateFinalAgentTaskUsageTotals(&app.agentTasks[index])
	}

	for _, steps := range app.workflowSteps {
		for index := range steps {
			app.hydrateFinalAgentTaskUsageTotals(&steps[index].AgentTask)
		}
	}
}

func (app *App) hydrateFinalAgentTaskUsageTotals(task *database.AgentTaskEntity) {
	if usageTotals, ok := decodeFinalUsageTotals(task.UsageJSON); ok {
		app.agentTaskUsageTotals[task.Task.ID] = usageTotals

		return
	}

	if isTerminalAgentTaskState(task.Task.State) {
		delete(app.agentTaskUsageTotals, task.Task.ID)
	}
}

func decodeFinalUsageTotals(usageJSON string) (model.UsageTotals, bool) {
	var usageTotals model.UsageTotals
	if strings.TrimSpace(usageJSON) == "" ||
		json.Unmarshal([]byte(usageJSON), &usageTotals) != nil ||
		!usageTotals.Reported {
		return emptyUsageTotals(), false
	}

	if _, err := usageTotals.TotalTokens(); err != nil {
		return emptyUsageTotals(), false
	}

	return usageTotals, true
}

func emptyUsageTotals() model.UsageTotals {
	return model.UsageTotals{
		InputTokens: 0, OutputTokens: 0, ProviderRoundTrips: 0, Reported: false,
	}
}

func usageTotalsSuffix(aggregate model.UsageAggregate, total int) string {
	if aggregate.Known == 0 {
		return ""
	}

	tokens, err := aggregate.Usage.TotalTokens()
	if err != nil {
		return ""
	}

	suffix := compactCount64(tokens)
	if aggregate.Known < total {
		suffix += "+"
	}

	return suffix + " tok"
}

func summaryElapsed(task *database.TaskEntity, now time.Time) string {
	if task == nil || task.StartedAt == nil {
		return taskQueuedLabel
	}

	end := now

	if isTerminalAgentTaskState(task.State) {
		if task.FinishedAt == nil {
			return "unknown"
		}

		end = *task.FinishedAt
	}

	duration := max(end.Sub(*task.StartedAt), 0)

	return duration.Round(time.Second).String()
}

func fitSummaryIdentity(opening, flexible, identitySuffix, elapsed, token string, width int) string {
	baseSuffix := identitySuffix
	if elapsed != "" {
		baseSuffix += " · " + elapsed
	}

	withToken := baseSuffix
	if token != "" {
		withToken += " · " + token
	}

	for _, suffix := range []string{withToken, baseSuffix, identitySuffix} {
		available := width - tui.Width(opening) - tui.Width(suffix)
		if available >= 1 {
			return opening + tui.Truncate(flexible, available) + suffix
		}
	}

	return tui.Truncate(opening+flexible+withToken, width)
}

func (app *App) openAgentTasksPanel(ctx context.Context) {
	items := app.agentTaskItemsFromSnapshot()
	app.openPanel(panel.New(
		panelAgentTasks,
		"Agent Tasks",
		"Enter inspects; Ctrl+C cancels; /agents profiles lists profiles",
		items,
		true,
	))

	if !app.agentTaskPanelSnapshotValid {
		app.requestTerminalRefresh(ctx)
	}
}

func (app *App) refreshAgentTasksPanel(_ context.Context) {
	app.refreshAgentTasksPanelFromSnapshot()
}

func (app *App) refreshAgentTasksPanelFromSnapshot() {
	if app.selectedPanelKind != panelAgentTasks || app.panel == nil ||
		!app.agentTaskPanelSnapshotValid {
		return
	}

	selected, _ := app.panel.SelectedValue()
	items := app.agentTaskItemsFromSnapshot()

	panelModel := panel.New(
		panelAgentTasks,
		"Agent Tasks",
		"Enter inspects; Ctrl+C cancels; /agents profiles lists profiles",
		items,
		true,
	)
	for index := range items {
		if items[index].Value == selected {
			panelModel.SetSelectedIndex(index)

			break
		}
	}

	app.panel = panelModel
}

func (app *App) agentTaskItemsFromSnapshot() []tui.ListItem {
	items := make([]tui.ListItem, 0, len(app.agentTaskPanelSnapshot))
	for index := range app.agentTaskPanelSnapshot {
		task := &app.agentTaskPanelSnapshot[index].Task
		items = append(items, tui.ListItem{
			Value:       task.ID,
			Title:       taskTitle(task),
			Description: taskDescription(task),
			Meta:        taskMeta(task, time.Now()),
		})
	}

	return items
}

func taskTitle(task *database.TaskEntity) string {
	return string(task.State) + "  " + task.ID
}

func taskDescription(task *database.TaskEntity) string {
	description := "background agent task"
	if task.ErrorMessage != "" {
		description = task.ErrorMessage
	} else if task.Result != "" {
		description = task.Result
	}

	description = strings.Join(strings.Fields(description), " ")

	runes := []rune(description)
	if len(runes) > agentTaskDescriptionLimit {
		description = string(runes[:agentTaskDescriptionLimit-1]) + "…"
	}

	return description
}

func taskMeta(task *database.TaskEntity, now time.Time) string {
	start := task.CreatedAt
	if task.StartedAt != nil {
		start = *task.StartedAt
	}

	end := now
	if task.FinishedAt != nil {
		end = *task.FinishedAt
	}

	return end.Sub(start).Round(time.Second).String()
}

func (app *App) inspectAgentTask(ctx context.Context, taskID string) error {
	if err := app.validateAgentTaskInspection(); err != nil {
		return err
	}

	task, found, err := app.runtime.AgentTask(ctx, taskID)
	if err != nil {
		return terminalError(err, agentTaskLoadOperation)
	}

	if !found {
		return fmt.Errorf("agent task %q not found", taskID)
	}

	nextSessionStack, ownerFound := app.agentTaskInspectionStack(task.Task.OwnerSessionID)
	if !ownerFound {
		return fmt.Errorf(
			"agent task %q belongs to session %q outside the current inspection path",
			taskID,
			task.Task.OwnerSessionID,
		)
	}

	if app.activePrompt != nil && len(app.agentTaskSessionStack) > 0 &&
		app.activePrompt.SessionID == app.sessionID {
		return errors.New("cannot leave an inspected agent session while its prompt is active")
	}

	if err := app.switchToAgentTaskSession(
		ctx,
		task.ChildSessionID,
		nextSessionStack,
		!isTerminalAgentTaskState(task.Task.State),
	); err != nil {
		return err
	}

	app.watchInspectedTaskIfRunning(ctx, task)

	app.closePanel()
	app.addSystemMessage("inspecting agent task: " + taskID + "; select main above to return")

	return nil
}

func (app *App) validateAgentTaskInspection() error {
	if app.authWorking || app.compacting || (app.working && app.activePrompt == nil) {
		return errors.New("cannot inspect an agent task while another operation is active")
	}

	if app.runtime == nil {
		return terminalError(errors.New("runtime is not configured"), agentTaskLoadOperation)
	}

	return nil
}

func (app *App) switchToAgentTaskSession(
	ctx context.Context,
	sessionID string,
	sessionStack []string,
	preserveTransientState bool,
) error {
	settings, settingsFound, err := app.sessionSettings(ctx, sessionID)
	if err != nil {
		return terminalError(err, "load agent session")
	}

	messages, err := app.sessionMessages(ctx, sessionID)
	if err != nil {
		return terminalError(err, "load agent session")
	}

	app.stopAgentTaskWatches()
	app.invalidateTerminalRefresh()
	app.saveSessionView()
	app.agentTaskSessionStack = sessionStack

	if app.restoreSessionView(sessionID) {
		promptHistory := slices.Clone(app.promptHistory)
		promptHistoryImages := cloneImageAttachmentGroups(app.promptHistoryImages)
		promptHistoryDraft := app.promptHistoryDraft
		promptHistoryDraftImages := cloneImageAttachments(app.promptHistoryDraftImages)
		promptHistoryIndex := app.promptHistoryIndex
		app.transcript.History = nil
		app.transcript.LineCache.reset()
		app.appendSessionMessages(messages)
		app.promptHistory = promptHistory
		app.promptHistoryImages = promptHistoryImages
		app.promptHistoryDraft = promptHistoryDraft
		app.promptHistoryDraftImages = promptHistoryDraftImages
		app.promptHistoryIndex = promptHistoryIndex
		messages = nil

		if !preserveTransientState {
			app.resetStreamingBlocks()
			app.streamingText = ""
			app.streamingThinkingText = ""
			app.streamedToolEvents = 0
		}
	} else {
		app.sessionID = sessionID
		app.pendingParentID = nil
		app.resetMessages()
		app.resetStreamingBlocks()
		app.liveAgentCompletions = nil
		app.queuedMessages = nil
		app.steeringMessages = nil
		app.hiddenQueuedMessages = nil
		app.composerBuffer = tui.NewTextArea()
		app.statusMessage = ""
	}

	if settingsFound {
		app.applySessionSettings(&settings)
	}

	app.appendSessionMessages(messages)

	return nil
}

func (app *App) watchInspectedTaskIfRunning(ctx context.Context, task *database.AgentTaskEntity) {
	app.inspectedAgentTaskID = ""
	if task != nil && !isTerminalAgentTaskState(task.Task.State) {
		app.inspectedAgentTaskID = task.Task.ID
		app.watchInspectedAgentTask(ctx, task.Task.ID)
	}
}

func (app *App) agentTaskInspectionStack(ownerSessionID string) ([]string, bool) {
	if ownerSessionID == app.sessionID {
		stack := slices.Clone(app.agentTaskSessionStack)

		return append(stack, app.sessionID), true
	}

	for index, sessionID := range slices.Backward(app.agentTaskSessionStack) {
		if sessionID == ownerSessionID {
			return slices.Clone(app.agentTaskSessionStack[:index+1]), true
		}
	}

	if len(app.agentTaskSessionStack) > 0 &&
		ownerSessionID != "" && ownerSessionID == app.agentTaskSummaryOwnerID {
		stack := slices.Clone(app.agentTaskSessionStack)
		if len(stack) == 0 || stack[len(stack)-1] != ownerSessionID {
			stack = append(stack, ownerSessionID)
		}

		return stack, true
	}

	return nil, false
}

func (app *App) navigateToMainSession(ctx context.Context) error {
	if len(app.agentTaskSessionStack) == 0 {
		return nil
	}

	if app.activePrompt != nil && app.activePrompt.SessionID == app.sessionID {
		return errors.New("cannot leave an inspected agent session while its prompt is active")
	}

	rootSessionID := app.agentTaskSessionStack[0]
	if err := app.switchToAgentTaskSession(ctx, rootSessionID, nil, false); err != nil {
		return err
	}

	app.inspectedAgentTaskID = ""
	app.addSystemMessage("returned to main session")
	app.requestTerminalRefresh(ctx)

	return nil
}

func (app *App) leaveAgentTaskSession(ctx context.Context) error {
	if len(app.agentTaskSessionStack) == 0 {
		return errors.New("not inspecting an agent task")
	}

	last := len(app.agentTaskSessionStack) - 1
	parentSessionID := app.agentTaskSessionStack[last]

	settings, settingsFound, err := app.sessionSettings(ctx, parentSessionID)
	if err != nil {
		return terminalError(err, "load parent session")
	}

	messages, err := app.sessionMessages(ctx, parentSessionID)
	if err != nil {
		return terminalError(err, "load parent session")
	}

	app.stopAgentTaskWatches()
	app.invalidateTerminalRefresh()
	app.inspectedAgentTaskID = ""
	app.saveSessionView()
	app.agentTaskSessionStack = app.agentTaskSessionStack[:last]

	if app.restoreSessionView(parentSessionID) {
		app.appendMissingSessionMessages(messages)
	} else {
		app.sessionID = parentSessionID
		app.pendingParentID = nil
		app.resetMessages()
		app.resetStreamingBlocks()

		if settingsFound {
			app.applySessionSettings(&settings)
		}

		app.appendSessionMessages(messages)
	}

	app.addSystemMessage("returned to parent session")

	if len(app.agentTaskSessionStack) == 0 {
		app.requestTerminalRefresh(ctx)
	} else {
		app.resumeInspectedAgentTask(ctx, parentSessionID)
	}

	return nil
}

func (app *App) appendMissingSessionMessages(messages []database.SessionMessageEntity) {
	appended := false

	for index := range messages {
		message := &messages[index]
		if app.hasSessionMessage(message) {
			continue
		}

		app.appendMessage(chatMessageFromSessionMessage(message))

		appended = true

		if message.Role == database.RoleUser {
			app.recordPromptDraftHistory(promptDraft{
				Text: message.Content, Images: imageAttachmentsFromDatabase(message.Parts),
			})
		}
	}

	if appended {
		slices.SortStableFunc(app.transcript.History, func(left, right chatMessage) int {
			return left.CreatedAt.Compare(right.CreatedAt)
		})
		app.transcript.LineCache.reset()
	}
}

func (app *App) hasSessionMessage(message *database.SessionMessageEntity) bool {
	role := transcript.FromDatabaseRole(message.Role)

	for index := range app.transcript.History {
		history := &app.transcript.History[index]
		if message.EntryID != "" && history.EntryID != nil && *history.EntryID == message.EntryID {
			return true
		}

		if history.EntryID == nil && history.CreatedAt.Equal(message.CreatedAt) &&
			history.Role == role && history.Content == message.Content {
			if message.EntryID != "" {
				history.EntryID = cloneStringPtr(&message.EntryID)
			}

			return true
		}
	}

	return false
}

func (app *App) resumeInspectedAgentTask(ctx context.Context, childSessionID string) {
	ownerSessionID := app.agentTaskSessionStack[len(app.agentTaskSessionStack)-1]

	tasks, err := app.runtime.AgentTasks(ctx, ownerSessionID, agentTaskInlineLimit)
	if err != nil {
		app.addSystemMessage("failed to resume agent task activity: " + err.Error())

		return
	}

	for index := range tasks {
		if tasks[index].ChildSessionID == childSessionID {
			app.watchInspectedTaskIfRunning(ctx, &tasks[index])

			return
		}
	}
}

func (app *App) handleAgentTasksPanelKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if event.Key() != tcell.KeyCtrlC || app.panel == nil {
		return false, nil
	}

	taskID, ok := app.panel.SelectedValue()
	if !ok {
		return true, nil
	}

	agentTask, found, err := app.runtime.AgentTask(ctx, taskID)
	if err != nil {
		return true, terminalError(err, agentTaskLoadOperation)
	}

	if !found || agentTask.Task.OwnerSessionID != app.sessionID {
		return true, fmt.Errorf("agent task %q not found", taskID)
	}

	if _, _, err = app.runtime.CancelAgentTask(ctx, app.sessionID, taskID); err != nil {
		return true, terminalError(err, "cancel agent task")
	}

	app.setStatus("cancel requested: " + taskID)
	app.refreshAgentTasksPanel(ctx)

	return true, nil
}
