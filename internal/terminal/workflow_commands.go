package terminal

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/transcript"
)

const workflowInspectionLimit = 100

type workflowInspector interface {
	Get(context.Context, string) (*database.WorkflowRunEntity, bool, error)
	List(context.Context, string, int) ([]database.WorkflowRunEntity, error)
	Events(context.Context, string, int64, int) ([]database.TaskEventEntity, error)
	AgentTasks(context.Context, string) ([]database.WorkflowAgentTaskEntity, error)
}

func (app *App) showWorkflows(ctx context.Context, runID string) error {
	if app.workflows == nil {
		app.addSystemMessage("workflows: unavailable")

		return nil
	}

	if runID != "" {
		return app.showWorkflow(ctx, runID)
	}

	if app.sessionID == "" {
		app.addSystemMessage("workflows: no active session")

		return nil
	}

	runs, err := app.workflows.List(ctx, app.sessionID, workflowInspectionLimit)
	if err != nil {
		return terminalError(err, "list workflow runs")
	}

	if len(runs) == 0 {
		app.addSystemMessage("workflows: none")

		return nil
	}

	lines := make([]string, len(runs))
	for index := range runs {
		lines[index] = formatWorkflowRun(&runs[index])
	}

	app.addMessage(transcript.RoleCustom, strings.Join(lines, "\n"))

	return nil
}

func (app *App) showWorkflow(ctx context.Context, runID string) error {
	run, found, err := app.workflows.Get(ctx, runID)
	if err != nil {
		return terminalError(err, "load workflow run")
	}

	if !found || run.Task.OwnerSessionID != app.sessionID {
		app.addSystemMessage("workflow: not found")

		return nil
	}

	events, err := app.workflows.Events(ctx, runID, 0, workflowInspectionLimit)
	if err != nil {
		return terminalError(err, "load workflow events")
	}

	children, err := app.workflows.AgentTasks(ctx, runID)
	if err != nil {
		return terminalError(err, "load workflow children")
	}

	app.addMessage(transcript.RoleCustom, formatWorkflowInspection(run, children, events))

	return nil
}

func formatWorkflowRun(run *database.WorkflowRunEntity) string {
	return strings.Join([]string{
		run.Task.ID,
		"  name: " + run.Name,
		"  state: " + string(run.Task.State),
		"  updated: " + run.Task.UpdatedAt.Format("2006-01-02 15:04:05"),
	}, "\n")
}

type workflowResultView struct {
	Value  any    `json:"value"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

func formatWorkflowInspection(
	run *database.WorkflowRunEntity,
	children []database.WorkflowAgentTaskEntity,
	events []database.TaskEventEntity,
) string {
	lines := []string{
		formatWorkflowRun(run),
		"  source version: " + run.SourceVersion,
		"  elapsed: " + formatWorkflowElapsed(&run.Task),
		"  children: " + strconv.Itoa(len(children)),
	}
	for index := range children {
		child := &children[index]
		lines = append(lines, "    "+child.NodeKey+"["+strconv.Itoa(child.InvocationIndex)+"]: "+child.AgentTaskID)
	}

	lines = append(lines, "  events: "+strconv.Itoa(len(events)))
	for index := range events {
		event := &events[index]

		line := "    " + strconv.FormatInt(event.Sequence, 10) + ": " + event.Event.Kind
		if !event.Event.CreatedAt.IsZero() {
			line += " @ " + event.Event.CreatedAt.Format("2006-01-02 15:04:05")
		}

		if event.Event.PayloadJSON != "" && event.Event.PayloadJSON != "{}" {
			line += " " + event.Event.PayloadJSON
		}

		lines = append(lines, line)
	}

	lines = appendWorkflowResult(lines, run.Task.Result)

	if run.Task.ErrorCode != "" || run.Task.ErrorMessage != "" {
		lines = append(lines, "  error: "+strings.TrimSpace(run.Task.ErrorCode+" "+run.Task.ErrorMessage))
	}

	return strings.Join(lines, "\n")
}

func formatWorkflowElapsed(task *database.TaskEntity) string {
	if task.StartedAt == nil {
		return "-"
	}

	end := task.UpdatedAt
	if task.FinishedAt != nil {
		end = *task.FinishedAt
	}

	if end.Before(*task.StartedAt) {
		return "-"
	}

	return end.Sub(*task.StartedAt).Round(time.Millisecond).String()
}

func appendWorkflowResult(lines []string, raw string) []string {
	if raw == "" {
		return lines
	}

	var result workflowResultView
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return append(lines, "  result: "+raw)
	}

	if result.Value != nil {
		if value, err := json.Marshal(result.Value); err == nil {
			lines = append(lines, "  value: "+string(value))
		}
	}

	if result.Stdout != "" {
		lines = append(lines, "  stdout: "+result.Stdout)
	}

	if result.Stderr != "" {
		lines = append(lines, "  stderr: "+result.Stderr)
	}

	return lines
}
