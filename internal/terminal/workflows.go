package terminal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	panelWorkflows     panel.Kind = "workflows"
	workflowPanelLimit int        = 100
	workflowEventLimit int        = 500
	workflowRunPrefix  string     = "run:"
	workflowTaskPrefix string     = "task:"
)

type workflowProgress struct {
	Total     int
	Succeeded int
	Failed    int
	Running   int
}

type workflowDetails struct {
	ProgressByRun map[string]workflowProgress
	StepsByRun    map[string][]database.WorkflowAgentTaskDetail
}

func (app *App) openWorkflowsPanel(ctx context.Context) {
	items, err := app.workflowItems(ctx)
	if err != nil {
		app.addSystemMessage(err.Error())

		return
	}

	app.workflowPanelRunID = ""
	app.openPanel(panel.New(
		panelWorkflows,
		"Workflows",
		"Enter inspects; Ctrl+C cancels",
		items,
		true,
	))
}

func (app *App) refreshWorkflowsPanel(ctx context.Context) {
	if app.selectedPanelKind != panelWorkflows || app.panel == nil {
		return
	}

	if app.workflowPanelRunID != "" {
		selected, hasSelection := app.panel.SelectedValue()
		if err := app.openWorkflowDetail(ctx, app.workflowPanelRunID); err != nil {
			app.setStatus(err.Error())

			return
		}

		if hasSelection {
			app.restoreWorkflowPanelSelection(selected)
		}

		return
	}

	selected, _ := app.panel.SelectedValue()

	items, err := app.workflowItems(ctx)
	if err != nil {
		app.setStatus(err.Error())

		return
	}

	app.panel = panel.New(panelWorkflows, "Workflows", "Enter inspects; Ctrl+C cancels", items, true)
	app.restoreWorkflowPanelSelection(selected)
}

func (app *App) restoreWorkflowPanelSelection(selected string) {
	for index, item := range app.panel.Items() {
		if item.Value == selected {
			app.panel.SetSelectedIndex(index)

			return
		}
	}
}

func (app *App) workflowItems(ctx context.Context) ([]tui.ListItem, error) {
	if app.workflows == nil || app.sessionID == "" {
		return []tui.ListItem{}, nil
	}

	runs, err := app.workflows.List(ctx, app.sessionID, workflowPanelLimit)
	if err != nil {
		return nil, terminalError(err, "list workflows")
	}

	runIDs := make([]string, len(runs))
	for index := range runs {
		runIDs[index] = runs[index].Task.ID
	}

	details, detailsErr := app.loadWorkflowDetails(ctx, runIDs)
	if detailsErr != nil {
		return nil, detailsErr
	}

	items := make([]tui.ListItem, 0, len(runs))
	for index := range runs {
		run := &runs[index]
		progress := details.ProgressByRun[run.Task.ID]
		app.workflowProgress[run.Task.ID] = progress

		items = append(items, tui.ListItem{
			Value:       workflowRunPrefix + run.Task.ID,
			Title:       workflowTitle(run),
			Description: workflowDescription(run, progress),
			Meta:        taskMeta(&run.Task, time.Now()),
		})
	}

	return items, nil
}

func (app *App) openWorkflowDetail(ctx context.Context, runID string) error {
	if app.workflows == nil {
		return errors.New("workflow runtime is not configured")
	}

	run, found, err := app.workflows.Get(ctx, runID)
	if err != nil {
		return terminalError(err, "load workflow")
	}

	if !found || run.Task.OwnerSessionID != app.sessionID {
		return fmt.Errorf("workflow %q not found", runID)
	}

	details, err := app.workflows.AgentTaskDetails(ctx, []string{runID})
	if err != nil {
		return terminalError(err, "list workflow agent task details")
	}

	items := make([]tui.ListItem, 0, len(details)+1)
	items = append(items, tui.ListItem{
		Value:       workflowRunPrefix + runID,
		Title:       workflowTitle(run),
		Description: workflowRunOutcome(run),
		Meta:        taskMeta(&run.Task, time.Now()),
	})

	for index := range details {
		detail := &details[index]
		link := &detail.Link
		task := &detail.AgentTask

		node := strings.TrimSpace(link.NodeKey)
		if node == "" {
			node = "agent"
		}

		items = append(items, tui.ListItem{
			Value:       workflowTaskPrefix + task.Task.ID,
			Title:       fmt.Sprintf("%s[%d]  %s", node, link.InvocationIndex, task.Task.State),
			Description: agentTaskSummaryLabel(task),
			Meta:        taskMeta(&task.Task, time.Now()),
		})
	}

	app.workflowPanelRunID = runID
	app.openPanel(panel.New(
		panelWorkflows,
		"Workflow: "+workflowName(run),
		"Enter inspects an agent; Esc returns; Ctrl+C cancels workflow",
		items,
		true,
	))

	return nil
}

func (app *App) applyWorkflowSelection(ctx context.Context, value string) error {
	if taskID, ok := strings.CutPrefix(value, workflowTaskPrefix); ok {
		return app.inspectAgentTask(ctx, taskID)
	}

	if runID, ok := strings.CutPrefix(value, workflowRunPrefix); ok {
		return app.openWorkflowDetail(ctx, runID)
	}

	return fmt.Errorf("unknown workflow selection %q", value)
}

func (app *App) handleWorkflowsPanelKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if event.Key() != tcell.KeyCtrlC || app.panel == nil {
		return false, nil
	}

	runID := app.workflowPanelRunID
	if runID == "" {
		value, selected := app.panel.SelectedValue()
		if !selected {
			return true, nil
		}

		runID, selected = strings.CutPrefix(value, workflowRunPrefix)
		if !selected {
			return true, nil
		}
	}

	changed, err := app.workflows.Cancel(ctx, app.sessionID, runID)
	if err != nil {
		return true, terminalError(err, "cancel workflow")
	}

	if changed {
		app.setStatus("workflow cancel requested: " + runID)
	}

	return true, nil
}

func (app *App) loadWorkflowDetails(
	ctx context.Context,
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

	details, err := app.workflows.AgentTaskDetails(ctx, runIDs)
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

func workflowTitle(run *database.WorkflowRunEntity) string {
	return string(run.Task.State) + "  " + workflowName(run)
}

func workflowName(run *database.WorkflowRunEntity) string {
	name := strings.Join(strings.Fields(run.Name), " ")
	if name == "" {
		return "workflow"
	}

	return name
}

func workflowDescription(run *database.WorkflowRunEntity, progress workflowProgress) string {
	outcome := workflowRunOutcome(run)

	counts := fmt.Sprintf("%d/%d agents", progress.Succeeded+progress.Failed, progress.Total)
	if progress.Failed > 0 {
		counts += fmt.Sprintf(" · %d failed", progress.Failed)
	}

	if outcome == "" {
		return counts
	}

	return counts + " · " + outcome
}

func workflowRunOutcome(run *database.WorkflowRunEntity) string {
	if run.Task.ErrorMessage != "" {
		return strings.Join(strings.Fields(run.Task.ErrorMessage), " ")
	}

	if run.Task.Result != "" {
		return strings.Join(strings.Fields(run.Task.Result), " ")
	}

	return "durable dynamic workflow"
}
