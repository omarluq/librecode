package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/workflow"
)

const workflowTaskIDKey = "workflow_task_id"

// WorkflowSubmitter is the runtime-facing boundary for durable workflows.
type WorkflowSubmitter interface {
	Submit(context.Context, *workflow.ServiceRequest) (*database.WorkflowRunEntity, error)
}

func workflowOutcomeResult(kind ExecutionResultKind) tool.Result {
	return tool.TextResult("", executionResultDetails(nil, MVMExecutionProfileDurable, kind))
}

func workflowResultDetails(run *database.WorkflowRunEntity) map[string]any {
	if run == nil {
		return map[string]any{}
	}

	return executionResultDetails(map[string]any{
		"run_id": run.Task.ID, workflowTaskIDKey: run.Task.ID,
		"kind": database.TaskKindWorkflow, executeNameKey: run.Name, "state": run.Task.State,
	}, MVMExecutionProfileDurable, ExecutionResultAccepted)
}
