package terminal

import (
	"context"

	"github.com/gdamore/tcell/v3"
)

const (
	agentTaskSummaryPageItems    = 5
	agentTaskSummaryMainIndex    = 0
	agentTaskSummaryContentStart = 1
)

type agentTaskSummarySelection struct {
	ItemIndex int
	Active    bool
}

func (app *App) agentTaskSummaryFocused() bool {
	return app.agentTaskSummarySelection.Active
}

func (app *App) blurAgentTaskSummary() {
	app.agentTaskSummarySelection = agentTaskSummarySelection{ItemIndex: 0, Active: false}
}

func (app *App) focusAgentTaskSummary() bool {
	if app.agentTaskSummaryItemCount() == 0 {
		return false
	}

	app.agentTaskSummarySelection = agentTaskSummarySelection{ItemIndex: 0, Active: true}

	return true
}

func (app *App) handleAgentTaskSummaryPriorityKey(
	ctx context.Context,
	event *tcell.EventKey,
) (bool, error) {
	if app.workflowSummaryRunID != "" && app.agentTaskSummaryFocused() &&
		app.keys.matches(event, actionSelectCancel) {
		app.workflowSummaryRunID = ""
		app.agentTaskSummarySelection.ItemIndex = 1

		return true, nil
	}

	if app.agentTaskSummaryFocused() && app.keys.matches(event, actionSelectConfirm) {
		return true, app.confirmAgentTaskSummarySelection(ctx)
	}

	return app.handleInlineListKey(
		event,
		app.agentTaskSummaryFocused(),
		app.focusAgentTaskSummary,
		app.moveAgentTaskSummarySelection,
		app.blurAgentTaskSummary,
		agentTaskSummaryPageItems,
	), nil
}

func (app *App) confirmAgentTaskSummarySelection(ctx context.Context) error {
	if app.workflowSummaryRunID != "" {
		return nil
	}

	index := app.agentTaskSummarySelection.ItemIndex
	if index == agentTaskSummaryMainIndex {
		if err := app.navigateToMainSession(ctx); err != nil {
			return err
		}

		app.blurAgentTaskSummary()

		return nil
	}

	workflowIndex := index - agentTaskSummaryContentStart
	if workflowIndex < len(app.activeWorkflows) {
		app.workflowSummaryRunID = app.activeWorkflows[workflowIndex].Task.ID
		app.agentTaskSummarySelection.ItemIndex = agentTaskSummaryContentStart

		return nil
	}

	taskID, ok := app.selectedAgentTaskSummaryTaskID()
	if !ok {
		return nil
	}

	if err := app.inspectAgentTask(ctx, taskID); err != nil {
		return err
	}

	app.blurAgentTaskSummary()

	return nil
}

func (app *App) moveAgentTaskSummarySelection(delta int) {
	count := app.agentTaskSummaryItemCount()
	if count == 0 {
		app.blurAgentTaskSummary()

		return
	}

	app.agentTaskSummarySelection.ItemIndex = min(
		max(0, app.agentTaskSummarySelection.ItemIndex+delta),
		count-1,
	)
}

func (app *App) selectedAgentTaskSummaryTaskID() (string, bool) {
	index := app.agentTaskSummarySelection.ItemIndex - agentTaskSummaryContentStart - len(app.activeWorkflows)
	if index < 0 {
		return "", false
	}

	for taskIndex := range app.agentTasks {
		task := &app.agentTasks[taskIndex]
		if task.Task.ParentTaskID != "" {
			continue
		}

		if index == 0 {
			return task.Task.ID, true
		}

		index--
	}

	return "", false
}

func (app *App) agentTaskSummaryItemCount() int {
	if len(app.activeWorkflows) == 0 && len(app.agentTasks) == 0 {
		return 0
	}

	if app.workflowSummaryRunID != "" {
		return agentTaskSummaryContentStart + 1
	}

	count := agentTaskSummaryContentStart + len(app.activeWorkflows)
	for index := range app.agentTasks {
		if app.agentTasks[index].Task.ParentTaskID == "" {
			count++
		}
	}

	return count
}

func (app *App) validateAgentTaskSummarySelection() bool {
	if !app.agentTaskSummaryFocused() {
		return false
	}

	count := app.agentTaskSummaryItemCount()
	if count == 0 {
		app.blurAgentTaskSummary()

		return false
	}

	app.agentTaskSummarySelection.ItemIndex = min(
		max(0, app.agentTaskSummarySelection.ItemIndex),
		count-1,
	)

	return true
}
