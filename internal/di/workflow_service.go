package di

import (
	"context"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/workflow"
)

// WorkflowService owns script-driven durable agent orchestration.
type WorkflowService struct {
	Runner     *workflow.Runner
	Runs       *workflow.Service
	database   *DatabaseService
	assistant  *AssistantService
	agentTasks *AgentTaskService
}

// NewWorkflowService wires workflow execution to the durable agent scheduler.
func NewWorkflowService(injector do.Injector) (*WorkflowService, error) {
	databaseService, err := do.Invoke[*DatabaseService](injector)
	if err != nil {
		return nil, serviceError(err, "resolve database service")
	}

	assistantService, err := do.Invoke[*AssistantService](injector)
	if err != nil {
		return nil, serviceError(err, "resolve assistant service")
	}

	agentTaskService, err := do.Invoke[*AgentTaskService](injector)
	if err != nil {
		return nil, serviceError(err, "resolve agent task service")
	}

	return &WorkflowService{
		Runner: nil, Runs: nil, database: databaseService, assistant: assistantService, agentTasks: agentTaskService,
	}, nil
}

// Start constructs workflow orchestration and recovers interrupted runs.
func (service *WorkflowService) Start(ctx context.Context) error {
	if service.Runs != nil {
		return nil
	}

	submitter, err := assistant.NewAgentSubmitter(
		service.agentTasks.Tasks,
		service.assistant.Agents,
	)
	if err != nil {
		return serviceError(err, "create workflow agent submitter")
	}

	controller, err := assistant.NewWorkflowController(
		submitter,
		service.agentTasks.Tasks,
		service.database.Sessions,
	)
	if err != nil {
		return serviceError(err, "create workflow controller")
	}

	runner, err := workflow.NewRunner(controller)
	if err != nil {
		return serviceError(err, "create workflow runner")
	}

	runs, err := workflow.NewService(service.database.Workflows, runner)
	if err != nil {
		return serviceError(err, "create durable workflow service")
	}

	if _, recoverErr := runs.RecoverInterrupted(ctx); recoverErr != nil {
		return serviceError(recoverErr, "recover interrupted workflows")
	}

	service.Runner = runner
	service.Runs = runs

	return nil
}
