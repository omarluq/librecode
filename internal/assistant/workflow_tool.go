package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/workflow"
)

const workflowToolName tool.Name = "workflow"

const workflowTaskIDKey = "workflow_task_id"

type workflowSubmitter interface {
	Submit(context.Context, *workflow.ServiceRequest) (*database.WorkflowRunEntity, error)
}

type workflowToolExecutor struct {
	submitter      workflowSubmitter
	ownerSessionID string
}

type workflowToolInput struct {
	Arguments map[string]any `json:"arguments,omitempty"`
	Name      string         `json:"name,omitempty"`
	Source    string         `json:"source"`
}

func (executor *workflowToolExecutor) Definition() tool.Definition {
	const rawSchema = `{
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"name":{"type":"string","description":"Concise workflow name."},
			"source":{"type":"string","description":"MVM Go source importing librecode/workflow."},
			"arguments":{"type":"object","description":"Values exposed as workflow.Arguments."}
		},
		"required":["name","source"]
	}`

	return tool.Definition{
		Schema: mustToolSchema(rawSchema), Name: workflowToolName, Label: "Start workflow",
		Description:   "Start a durable asynchronous workflow authored as MVM Go code and return its run ID.",
		PromptSnippet: "Use workflow for durable multi-agent orchestration",
		PromptGuidelines: []string{
			`Write MVM Go source that imports "librecode/workflow".`,
			"Use workflow.Agent, workflow.Wait, workflow.List, workflow.Cancel, or workflow.Pipeline.",
			"Use workflows for staged or parallel subagent work; the run continues independently of the model turn.",
		},
		ReadOnly: false,
	}
}

func (executor *workflowToolExecutor) Execute(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	if executor.submitter == nil {
		return tool.TextResult("", nil), oops.In("assistant").Code("workflow_service_unavailable").
			Errorf("workflow service is unavailable")
	}

	var args workflowToolInput
	if err := input.Decode(&args); err != nil {
		return tool.TextResult("", nil), oops.In("assistant").Code("workflow_input").Wrapf(err, "decode workflow input")
	}

	args.Name = strings.TrimSpace(args.Name)
	args.Source = strings.TrimSpace(args.Source)

	if args.Name == "" {
		return tool.TextResult("", nil), oops.In("assistant").Code("workflow_name_required").
			Errorf("workflow name is required")
	}

	if args.Source == "" {
		return tool.TextResult("", nil), oops.In("assistant").Code("workflow_source_required").
			Errorf("workflow source is required")
	}

	if !utf8.ValidString(args.Source) {
		return tool.TextResult("", nil), oops.In("assistant").Code("workflow_source_invalid_utf8").
			Errorf("workflow source must be valid UTF-8")
	}

	arguments := args.Arguments
	if arguments == nil {
		arguments = map[string]any{}
	}

	argumentsJSON, err := json.Marshal(arguments)
	if err != nil {
		return tool.TextResult("", nil), oops.In("assistant").Code("encode_workflow_arguments").
			Wrapf(err, "encode workflow arguments")
	}

	run, err := executor.submitter.Submit(ctx, &workflow.ServiceRequest{
		Name: args.Name, Source: args.Source, SourceVersion: "v1",
		ArgumentsJSON: string(argumentsJSON), OwnerSessionID: executor.ownerSessionID,
	})
	if err != nil {
		return tool.TextResult("", nil), oops.In("assistant").Code("submit_workflow").Wrapf(err, "submit workflow")
	}

	return tool.TextResult(
		fmt.Sprintf("Started workflow %q with run ID %s.", run.Name, run.Task.ID),
		workflowResultDetails(run),
	), nil
}

func workflowResultDetails(run *database.WorkflowRunEntity) map[string]any {
	if run == nil {
		return map[string]any{}
	}

	return map[string]any{
		"run_id": run.Task.ID, workflowTaskIDKey: run.Task.ID,
		"kind": database.TaskKindWorkflow, executeNameKey: run.Name, "state": run.Task.State,
	}
}

var _ tool.Executor = (*workflowToolExecutor)(nil)
