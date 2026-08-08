package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tooltask"
	"github.com/omarluq/librecode/internal/transcript"
)

const (
	toolTaskListLimit   = 100
	toolTaskCancelParts = 2
)

func (app *App) watchToolTaskCompletions(ctx context.Context) {
	if app.deliveredToolTasks == nil {
		app.deliveredToolTasks = make(map[string]struct{})
	}

	if app.runtime == nil || app.cancelToolTaskCompletions != nil {
		return
	}

	events, cancel, err := app.runtime.SubscribeToolTaskCompletions()
	if err != nil {
		return
	}

	app.cancelToolTaskCompletions = cancel

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case completion, open := <-events:
				if !open {
					return
				}

				event := assistant.BackgroundToolCompletionEvent(&completion)
				app.postAsyncEvent(ctx, &asyncEvent{
					Response: nil, ToolCallEvent: nil, ToolEvent: &event, Usage: nil,
					Kind: asyncEventAgentTaskCompleted, Provider: completion.OwnerSessionID,
					Text: completion.TaskID, PromptID: 0,
				})
			}
		}
	}()
}

func (app *App) stopToolTaskCompletions() {
	if app.cancelToolTaskCompletions != nil {
		app.cancelToolTaskCompletions()
		app.cancelToolTaskCompletions = nil
	}
}

func (app *App) handleTaskCompletionEvent(ctx context.Context, payload *asyncEvent) {
	if payload.ToolEvent != nil {
		app.deliverToolTaskEvent(payload.Text, payload.Provider, payload.ToolEvent)

		return
	}

	app.deliverAgentTaskCompletionEvent(ctx, payload.Provider, payload.Text)
}

func (app *App) deliverToolTaskCompletion(completion *tooltask.Completion) {
	if completion == nil {
		return
	}

	event := assistant.BackgroundToolCompletionEvent(completion)
	app.deliverToolTaskEvent(completion.TaskID, completion.OwnerSessionID, &event)
}

func (app *App) deliverToolTaskEvent(taskID, ownerSessionID string, event *assistant.ToolEvent) {
	if taskID == "" || ownerSessionID == "" || event == nil {
		return
	}

	if app.deliveredToolTasks == nil {
		app.deliveredToolTasks = make(map[string]struct{})
	}

	if _, delivered := app.deliveredToolTasks[taskID]; delivered {
		return
	}

	if !app.withSessionView(ownerSessionID, func() {
		app.addMessage(transcript.RoleToolResult, formatToolEventForUI(event))
		app.deliveredToolTasks[taskID] = struct{}{}
	}) {
		// The originating session is not loaded. Its durable outcome remains
		// available through /tasks and the explicit background get action.
		app.setStatus("background tool result owner view is unavailable")
	}
}

func (app *App) refreshToolTasks(ctx context.Context) error {
	if app.runtime == nil || app.sessionID == "" {
		app.toolTasks = nil

		return nil
	}

	tasks, err := app.runtime.ToolTasks(ctx, app.sessionID, nil, toolTaskListLimit)
	if err != nil {
		return fmt.Errorf("refresh tool tasks: %w", err)
	}

	app.toolTasks = tasks

	return nil
}

func (app *App) logToolTaskRefreshError(ctx context.Context, err error) {
	if err != nil {
		slog.Default().ErrorContext(ctx, "refresh terminal tool tasks", "error", err)
	}
}

func (app *App) runToolTaskCommand(ctx context.Context, args string) error {
	if app.runtime == nil || app.sessionID == "" {
		return errors.New("task runtime is unavailable")
	}

	fields := strings.Fields(args)
	switch {
	case len(fields) > 0 && fields[0] == "cancel":
		return app.cancelToolTask(ctx, fields)
	case len(fields) > 1:
		return errors.New("usage: /tasks [task-id|cancel <task-id>]")
	case len(fields) == 1:
		return app.inspectToolTask(ctx, fields[0])
	default:
		return app.listToolTasks(ctx)
	}
}

func (app *App) cancelToolTask(ctx context.Context, fields []string) error {
	if len(fields) != toolTaskCancelParts {
		return errors.New("usage: /tasks cancel <task-id>")
	}

	task, found, err := app.runtime.CancelToolTask(ctx, app.sessionID, fields[1])
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}

	if !found {
		return fmt.Errorf("task %q not found", fields[1])
	}

	if err := app.refreshToolTasks(ctx); err != nil {
		return err
	}

	app.setStatus(fmt.Sprintf("task %s is %s", task.Task.ID, task.Task.State))

	return nil
}

func (app *App) inspectToolTask(ctx context.Context, taskID string) error {
	task, found, err := app.runtime.ToolTask(ctx, app.sessionID, taskID)
	if err != nil {
		return fmt.Errorf("inspect task: %w", err)
	}

	if !found {
		return fmt.Errorf("task %q not found", taskID)
	}

	app.addSystemMessage(formatToolTask(task))

	return nil
}

func (app *App) listToolTasks(ctx context.Context) error {
	if err := app.refreshToolTasks(ctx); err != nil {
		return err
	}

	if len(app.toolTasks) == 0 {
		app.setStatus("no durable background tasks")

		return nil
	}

	lines := make([]string, 0, len(app.toolTasks))
	for index := range app.toolTasks {
		lines = append(lines, formatToolTaskSummary(&app.toolTasks[index]))
	}

	app.addSystemMessage(strings.Join(lines, "\n"))

	return nil
}

func formatToolTaskSummary(task *database.ToolTaskEntity) string {
	return fmt.Sprintf("%s  %-11s %s", task.Task.ID, task.Task.State, task.TargetName)
}

func formatToolTask(task *database.ToolTaskEntity) string {
	parts := []string{formatToolTaskSummary(task)}
	if task.Task.ErrorMessage != "" {
		parts = append(parts, "error: "+task.Task.ErrorMessage)
	}

	if task.OutcomeJSON != nil {
		var outcome struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(*task.OutcomeJSON), &outcome) == nil {
			for _, block := range outcome.Result.Content {
				if block.Text != "" {
					parts = append(parts, block.Text)
				}
			}
		}
	}

	return strings.Join(parts, "\n")
}
