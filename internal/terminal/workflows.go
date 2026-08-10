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
	items := app.workflowItemsFromSnapshot()
	app.workflowPanelRunID = ""
	app.openPanel(panel.New(
		panelWorkflows,
		"Workflows",
		"Enter inspects; Ctrl+C cancels",
		items,
		true,
	))

	if !app.workflowPanelSnapshotValid || !app.workflowDetailSnapshotValid {
		app.requestTerminalRefresh(ctx)
	}
}

func (app *App) refreshWorkflowsPanel(_ context.Context) {
	app.refreshWorkflowsPanelFromSnapshot()
}

func (app *App) refreshWorkflowsPanelFromSnapshot() {
	if app.selectedPanelKind != panelWorkflows || app.panel == nil {
		return
	}

	if app.workflowPanelRunID != "" {
		selected, hasSelection := app.panel.SelectedValue()
		if err := app.openWorkflowDetailFromSnapshot(app.workflowPanelRunID); err != nil {
			return
		}

		if hasSelection {
			app.restoreWorkflowPanelSelection(selected)
		}

		return
	}

	selected, _ := app.panel.SelectedValue()

	if !app.workflowPanelSnapshotValid {
		return
	}

	items := app.workflowItemsFromSnapshot()
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

func (app *App) workflowItemsFromSnapshot() []tui.ListItem {
	items := make([]tui.ListItem, 0, len(app.workflowPanelSnapshot))
	for index := range app.workflowPanelSnapshot {
		run := &app.workflowPanelSnapshot[index]
		progress := app.workflowProgress[run.Task.ID]

		items = append(items, tui.ListItem{
			Value:       workflowRunPrefix + run.Task.ID,
			Title:       workflowTitle(run),
			Description: workflowDescription(run, progress),
			Meta:        taskMeta(&run.Task, time.Now()),
		})
	}

	return items
}

func (app *App) openWorkflowDetail(_ context.Context, runID string) error {
	return app.openWorkflowDetailFromSnapshot(runID)
}

func (app *App) openWorkflowDetailFromSnapshot(runID string) error {
	run := workflowByID(app.workflowPanelSnapshot, runID)
	if run == nil {
		run = workflowByID(app.activeWorkflows, runID)
	}

	if run == nil || run.Task.OwnerSessionID != app.sessionID {
		return fmt.Errorf("workflow %q not found", runID)
	}

	if !app.workflowDetailSnapshotValid {
		return errors.New("workflow details are loading")
	}

	details := app.workflowSteps[runID]

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

func workflowByID(runs []database.WorkflowRunEntity, runID string) *database.WorkflowRunEntity {
	for index := range runs {
		if runs[index].Task.ID == runID {
			return &runs[index]
		}
	}

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
	return loadWorkflowDetails(ctx, app.workflows, runIDs)
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
