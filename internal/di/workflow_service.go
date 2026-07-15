package di

import (
	"context"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/workflow"
)

// WorkflowService owns script-driven durable agent orchestration.
type WorkflowService struct {
	Runner *workflow.Runner
	Runs   *workflow.Service
}

// NewWorkflowService wires workflow execution to the durable agent scheduler.
func NewWorkflowService(injector do.Injector) (*WorkflowService, error) {
	databaseService := do.MustInvoke[*DatabaseService](injector)
	assistantService := do.MustInvoke[*AssistantService](injector)
	agentTaskService := do.MustInvoke[*AgentTaskService](injector)

	submitter, err := assistant.NewAgentSubmitter(
		agentTaskService.Tasks,
		databaseService.Sessions,
		assistantService.Agents,
	)
	if err != nil {
		return nil, serviceError(err, "create workflow agent submitter")
	}

	controller, err := assistant.NewWorkflowController(
		submitter,
		agentTaskService.Tasks,
		databaseService.Sessions,
	)
	if err != nil {
		return nil, serviceError(err, "create workflow controller")
	}

	runner, err := workflow.NewRunner(controller)
	if err != nil {
		return nil, serviceError(err, "create workflow runner")
	}

	runs, err := workflow.NewService(databaseService.Workflows, runner)
	if err != nil {
		return nil, serviceError(err, "create durable workflow service")
	}

	if _, recoverErr := runs.RecoverInterrupted(context.Background()); recoverErr != nil {
		return nil, serviceError(recoverErr, "recover interrupted workflows")
	}

	return &WorkflowService{Runner: runner, Runs: runs}, nil
}
