package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	app.agentTasksRefreshedAt = time.Time{}
	app.deliveredAgentTasks = map[string]struct{}{}
}

func (app *App) refreshVisibleAgentTasks(ctx context.Context) {
	if app.runtime == nil || app.sessionID == "" {
		app.agentTasks = nil
		app.activeWorkflows = nil

		return
	}

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

	active := make([]database.WorkflowRunEntity, 0, len(runs))

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

	for index := range runs {
		run, found := listed[runs[index].Task.ID]
		if found && !isTerminalAgentTaskState(run.Task.State) {
			active = append(active, run)
		}
	}

	app.activeWorkflows = active
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

	completion := fmt.Sprintf(
		"Workflow %q (%s) failed.\n\n%s\n\nUse /workflow %s for full details.",
		name, runID, detail, runID,
	)

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
	app.watchActiveAgentTasks(ctx)
}

func (app *App) watchActiveAgentTasks(ctx context.Context) {
	for index := range app.agentTasks {
		taskID := app.agentTasks[index].Task.ID
		if _, watching := app.agentTaskWatches[taskID]; watching {
			continue
		}

		events, cancelSubscription := app.runtime.SubscribeAgentTask(taskID)
		app.agentTaskWatches[taskID] = cancelSubscription

		go app.watchAgentTask(ctx, taskID, events, cancelSubscription)
	}
}

func (app *App) watchAgentTask(
	ctx context.Context,
	taskID string,
	events <-chan database.TaskEventEntity,
	cancelSubscription func(),
) {
	defer cancelSubscription()

	for {
		select {
		case event, open := <-events:
			if !open {
				return
			}

			if isTerminalAgentTaskEvent(event.Event.Kind) {
				app.postAgentTaskChanged(ctx, taskID)

				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (app *App) postAgentTaskChanged(ctx context.Context, taskID string) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventAgentTaskChanged,
		Provider:      "",
		Text:          taskID,
		PromptID:      0,
	})
}

func isTerminalAgentTaskEvent(kind string) bool {
	switch kind {
	case "task_succeeded", "task_failed", "task_canceled", "task_interrupted":
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

func (app *App) renderAgentTaskSummary(width int) []tui.Line {
	if len(app.agentTasks) == 0 && len(app.activeWorkflows) == 0 {
		return nil
	}

	indicatorStyle := tcell.StyleDefault.Foreground(defaultWorkingShimmerBrightColor()).Bold(true)
	labelStyle := tcell.StyleDefault.Foreground(app.theme.colors[colorMuted])

	lines := make([]tui.Line, 0, len(app.activeWorkflows)+len(app.agentTasks)+1)
	for index := range app.activeWorkflows {
		label := workflowSummaryLabel(&app.activeWorkflows[index])
		line := tui.Line{
			Text: pendingToolIndicator + " " + label, Style: labelStyle,
			Spans: []tui.Span{
				{Text: pendingToolIndicator, Style: indicatorStyle},
				{Text: " " + label, Style: labelStyle},
			},
		}
		lines = append(lines, line.Truncate(max(1, width)))
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
		lines = append(lines, line.Truncate(max(1, width)))
	}

	if len(lines) == 0 {
		return nil
	}

	lines = append(lines, tui.NewLine(tcell.StyleDefault, ""))

	return lines
}

func workflowSummaryLabel(run *database.WorkflowRunEntity) string {
	if run == nil {
		return "workflow"
	}

	name := strings.Join(strings.Fields(run.Name), " ")
	if name == "" {
		return "workflow"
	}

	return "workflow(" + name + ")"
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
	task, found, err := app.runtime.AgentTask(ctx, taskID)
	if err != nil {
		return terminalError(err, "load agent task")
	}

	if !found || task.Task.OwnerSessionID != app.sessionID {
		return fmt.Errorf("agent task %q not found", taskID)
	}

	app.agentTaskSessionStack = append(app.agentTaskSessionStack, app.sessionID)
	app.resetAgentTaskTracking()
	app.sessionID = task.ChildSessionID
	app.pendingParentID = nil
	app.resetMessages()

	if err := app.loadSessionSettings(ctx); err != nil {
		return terminalError(err, "load agent session")
	}

	if err := app.loadInitialMessages(ctx); err != nil {
		return terminalError(err, "load agent session")
	}

	app.closePanel()
	app.addSystemMessage("inspecting agent task: " + taskID + "; use /agents back to return")

	return nil
}

func (app *App) leaveAgentTaskSession(ctx context.Context) error {
	if len(app.agentTaskSessionStack) == 0 {
		return errors.New("not inspecting an agent task")
	}

	last := len(app.agentTaskSessionStack) - 1
	app.resetAgentTaskTracking()
	app.sessionID = app.agentTaskSessionStack[last]
	app.agentTaskSessionStack = app.agentTaskSessionStack[:last]
	app.pendingParentID = nil
	app.resetMessages()

	if err := app.loadSessionSettings(ctx); err != nil {
		return terminalError(err, "load parent session")
	}

	if err := app.loadInitialMessages(ctx); err != nil {
		return terminalError(err, "load parent session")
	}

	app.addSystemMessage("returned to parent session")

	return nil
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
		return true, terminalError(err, "load agent task")
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
