package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
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
)

const (
	workflowToolName    = "workflow"
	agentStartToolName  = "agent_start"
	agentStatusToolName = "agent_status"
	agentWaitToolName   = "agent_wait"
	agentCancelToolName = "agent_cancel"
	agentListToolName   = "agent_list"
)

func isAgentManagementTool(name string) bool {
	switch name {
	case agentStartToolName, agentStatusToolName, agentWaitToolName, agentCancelToolName, agentListToolName:
		return true
	default:
		return false
	}
}

func (app *App) applyAgentToolEvent(event *assistant.ToolEvent) {
	if event == nil || event.IsError || app.runtime == nil || app.sessionID == "" {
		return
	}

	if event.Name == agentStartToolName {
		app.trackStartedAgentTask(context.Background(), event)
		app.agentTasksRefreshedAt = time.Now()

		return
	}

	app.refreshVisibleAgentTasks(context.Background())
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
	app.agentTasks = nil
	app.activeWorkflows = nil
	app.workflowProgress = map[string]workflowProgress{}
	app.workflowSteps = map[string][]database.WorkflowAgentTaskDetail{}
	app.workflowSummaryRunID = ""
	app.agentTaskSummaryOwnerID = ""
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

	runs, err := app.workflows.List(ctx, app.sessionID, agentTaskInlineLimit)
	if err != nil {
		return
	}

	listed := make(map[string]database.WorkflowRunEntity, len(runs))
	for index := range runs {
		listed[runs[index].Task.ID] = runs[index]
	}

	active := app.reconcileTrackedWorkflows(ctx, listed, len(runs))
	for index := range runs {
		run, found := listed[runs[index].Task.ID]
		if found && !isTerminalAgentTaskState(run.Task.State) {
			active = append(active, run)
		}
	}

	app.activeWorkflows = active
	if !app.hasActiveWorkflow(app.workflowSummaryRunID) {
		app.workflowSummaryRunID = ""
	}

	app.refreshWorkflowSummaryDetails(ctx)
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
			app.deliverWorkflowFailure(ctx, &latest)

			continue
		}

		active = append(active, latest)
	}

	return active
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
		app.deliverWorkflowFailure(ctx, run)

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

func (app *App) deliverWorkflowFailure(ctx context.Context, run *database.WorkflowRunEntity) {
	if run == nil || run.Task.State != database.TaskFailed {
		return
	}

	runID := run.Task.ID
	if _, delivered := app.deliveredAgentTasks[runID]; delivered {
		return
	}

	app.deliveredAgentTasks[runID] = struct{}{}

	detail := strings.TrimSpace(run.Task.ErrorMessage)
	if detail == "" {
		detail = "No error detail was returned."
	}

	name := strings.TrimSpace(run.Name)
	if name == "" {
		name = toolDisplayWorkflow
	}

	completion := fmt.Sprintf("Workflow %q (%s) failed.\n\n%s", name, runID, detail)

	app.setStatus("workflow failed")

	content := formatToolEventForUI(&assistant.ToolEvent{
		CallID: "", ParentCallID: "", Sequence: 0, Name: "workflow_result",
		ArgumentsJSON: "", DetailsJSON: "", Result: completion, Error: detail, IsError: true,
	})
	app.addAgentCompletionMessage(content)
	app.persistAgentCompletion(ctx, content)

	prompt := completion + "\n\nA background workflow failed after it was submitted. " +
		"Report the failure and relevant next step to the user."
	if app.busy() {
		app.queuePrompt(prompt, false)

		return
	}

	app.sendPromptHidden(ctx, prompt)
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
	app.watchActiveAgentTasks(ctx)
}

func (app *App) watchActiveAgentTasks(ctx context.Context) {
	for index := range app.agentTasks {
		taskID := app.agentTasks[index].Task.ID
		if _, watching := app.agentTaskWatches[taskID]; watching {
			continue
		}

		events, cancelSubscription := app.runtime.SubscribeAgentTask(taskID)
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

	events, cancelSubscription := app.runtime.SubscribeAgentTask(taskID)
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
	app.postAsyncEvent(ctx, &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventAgentTaskReplayError, Provider: taskID,
		Text: "failed to replay agent task activity: " + err.Error(), PromptID: 0,
	})
}

func (app *App) handleAgentTaskWatchError(ctx context.Context, taskID, message string) {
	app.addSystemMessage(message)

	if len(app.agentTaskSessionStack) > 0 {
		app.stopAgentTaskWatch(taskID)
		app.reloadInspectedAgentTaskTranscript(ctx, taskID)

		return
	}

	// Refresh while the failed watch is still registered so the refresh cannot
	// immediately create another watcher that repeats the same replay failure.
	app.refreshVisibleAgentTasks(ctx)
	app.stopAgentTaskWatch(taskID)
}

func (app *App) postAgentTaskStreamEvent(ctx context.Context, event *database.TaskEventEntity) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventAgentTaskStream, Provider: event.TaskID, Text: event.Event.PayloadJSON, PromptID: 0,
	})
}

func (app *App) handleAgentTaskTerminalEvent(ctx context.Context, taskID string) {
	if len(app.agentTaskSessionStack) == 0 {
		app.refreshVisibleAgentTasks(ctx)

		return
	}

	app.refreshInspectedParentAgentTask(ctx, taskID)
	app.reloadInspectedAgentTaskTranscript(ctx, taskID)
}

func (app *App) applyInspectedAgentTaskEvent(ctx context.Context, taskID, payloadJSON string) {
	if taskID == "" || len(app.agentTaskSessionStack) == 0 || app.runtime == nil {
		return
	}

	task, found, err := app.runtime.AgentTask(ctx, taskID)
	if err != nil || !found || task.ChildSessionID != app.sessionID {
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
	app.addSystemMessage("inspecting agent task: " + taskID + "; use /agents back to return")
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

const agentTaskSucceededEvent = "task_succeeded"

func isTerminalAgentTaskEvent(kind string) bool {
	switch kind {
	case agentTaskSucceededEvent, "task_failed", "task_canceled", "task_interrupted":
		return true
	default:
		return false
	}
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
		result = "No result was returned."
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

	app.deliverAgentTaskCompletionText(ctx, task.Task.ID, completion)
}

func (app *App) deliverAgentTaskCompletionText(ctx context.Context, taskID, completion string) {
	if taskID == "" || completion == "" {
		return
	}

	if _, delivered := app.deliveredAgentTasks[taskID]; delivered {
		return
	}

	workflowChild := app.isTrackedWorkflowChild(taskID) || app.isPersistedWorkflowChild(ctx, taskID)
	app.discardAgentTaskCompletion(taskID)

	if workflowChild {
		return
	}

	app.deliverAgentTaskCompletions(ctx, []string{completion})
}

func (app *App) isTrackedWorkflowChild(taskID string) bool {
	for index := range app.agentTasks {
		if app.agentTasks[index].Task.ID == taskID {
			return app.agentTasks[index].Task.ParentTaskID != ""
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
	if app.busy() {
		app.queuePrompt(prompt, false)

		return
	}

	app.sendPromptHidden(ctx, prompt)
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
			Model:     "",
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
		if !isTerminalAgentTaskState(app.activeWorkflows[index].Task.State) {
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

	lines := make([]tui.Line, 0, len(app.activeWorkflows)+len(app.agentTasks)+1)
	selectedIndex := app.selectedAgentTaskSummaryIndex()
	selectableIndex := 0

	if run := app.expandedWorkflowSummaryRun(); run != nil {
		lines = app.renderWorkflowSummaryDetail(run, width, labelStyle, headerStyle, selectedIndex == 0)

		return padAgentTaskSummary(lines, app.agentTaskSummaryHeight())
	}

	for index := range app.activeWorkflows {
		run := &app.activeWorkflows[index]
		label := app.workflowSummaryLabel(run)
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

		label := agentTaskSummaryLabel(task)
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

func (app *App) agentTaskSummaryHeight() int {
	collapsedHeight := len(app.activeWorkflows) + 1
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
		reservedHeight = max(reservedHeight, stepRows+workflowDetailFixedRows)
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
	line = line.Truncate(max(1, width))
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
	if run == nil {
		return toolDisplayWorkflow
	}

	label := toolDisplayWorkflow + "(" + workflowName(run) + ")"

	progress, ok := app.workflowProgress[run.Task.ID]
	if !ok || progress.Total == 0 {
		return label
	}

	return fmt.Sprintf(
		"%s  %d/%d agents · %s",
		label,
		progress.Succeeded+progress.Failed,
		progress.Total,
		taskMeta(&run.Task, time.Now()),
	)
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

func (app *App) openAgentTasksPanel(ctx context.Context) {
	items, err := app.agentTaskItems(ctx)
	if err != nil {
		app.addSystemMessage(err.Error())

		return
	}

	app.openPanel(panel.New(
		panelAgentTasks,
		"Agent Tasks",
		"Enter inspects; Ctrl+C cancels; /agents profiles lists profiles",
		items,
		true,
	))
}

func (app *App) refreshAgentTasksPanel(ctx context.Context) {
	if app.selectedPanelKind != panelAgentTasks || app.panel == nil {
		return
	}

	selected, _ := app.panel.SelectedValue()

	items, err := app.agentTaskItems(ctx)
	if err != nil {
		app.setStatus(err.Error())

		return
	}

	model := panel.New(
		panelAgentTasks,
		"Agent Tasks",
		"Enter inspects; Ctrl+C cancels; /agents profiles lists profiles",
		items,
		true,
	)
	for index := range items {
		if items[index].Value == selected {
			model.SetSelectedIndex(index)

			break
		}
	}

	app.panel = model
}

func (app *App) agentTaskItems(ctx context.Context) ([]tui.ListItem, error) {
	if app.runtime == nil || app.sessionID == "" {
		return nil, nil
	}

	tasks, err := app.runtime.AgentTasks(ctx, app.sessionID, agentTaskPanelLimit)
	if err != nil {
		return nil, terminalError(err, "list agent tasks")
	}

	items := make([]tui.ListItem, 0, len(tasks))
	for index := range tasks {
		task := &tasks[index].Task
		items = append(items, tui.ListItem{
			Value:       task.ID,
			Title:       taskTitle(task),
			Description: taskDescription(task),
			Meta:        taskMeta(task, time.Now()),
		})
	}

	return items, nil
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
	if app.busy() || app.activePrompt != nil {
		return errors.New("cannot inspect an agent task while a prompt is active")
	}

	if app.runtime == nil {
		return terminalError(errors.New("runtime is not configured"), agentTaskLoadOperation)
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

	settings, settingsFound, err := app.sessionSettings(ctx, task.ChildSessionID)
	if err != nil {
		return terminalError(err, "load agent session")
	}

	messages, err := app.sessionMessages(ctx, task.ChildSessionID)
	if err != nil {
		return terminalError(err, "load agent session")
	}

	app.stopAgentTaskWatches()
	app.agentTaskSessionStack = nextSessionStack
	app.sessionID = task.ChildSessionID
	app.pendingParentID = nil
	app.resetMessages()
	app.resetStreamingBlocks()

	if settingsFound {
		app.applySessionSettings(&settings)
	}

	app.appendSessionMessages(messages)

	app.watchInspectedTaskIfRunning(ctx, task)

	app.closePanel()
	app.addSystemMessage("inspecting agent task: " + taskID + "; use /agents back to return")

	return nil
}

func (app *App) watchInspectedTaskIfRunning(ctx context.Context, task *database.AgentTaskEntity) {
	if task != nil && !isTerminalAgentTaskState(task.Task.State) {
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
	app.sessionID = parentSessionID
	app.agentTaskSessionStack = app.agentTaskSessionStack[:last]
	app.pendingParentID = nil
	app.resetMessages()
	app.resetStreamingBlocks()

	if settingsFound {
		app.applySessionSettings(&settings)
	}

	app.appendSessionMessages(messages)
	app.addSystemMessage("returned to parent session")

	if len(app.agentTaskSessionStack) == 0 {
		app.refreshVisibleAgentTasks(ctx)
	} else {
		app.resumeInspectedAgentTask(ctx, parentSessionID)
	}

	return nil
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
