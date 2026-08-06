package di

import (
	"context"
	"sync"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/workflow"
)

// WorkflowService owns script-driven durable agent orchestration.
type WorkflowService struct {
	runner     *workflow.Runner
	runs       *workflow.Service
	database   *DatabaseService
	assistant  *AssistantService
	agentTasks *AgentTaskService
	lifecycle  sync.Mutex
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
		runner: nil, runs: nil, database: databaseService, assistant: assistantService, agentTasks: agentTaskService,
		lifecycle: sync.Mutex{},
	}, nil
}

// Runner returns the constructed workflow runner, if startup reached that stage.
func (service *WorkflowService) Runner() *workflow.Runner {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	return service.runner
}

// Runs returns the constructed durable workflow service, if startup reached that stage.
func (service *WorkflowService) Runs() *workflow.Service {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	return service.runs
}

// Start constructs workflow orchestration and recovers interrupted runs.
func (service *WorkflowService) Start(ctx context.Context) error {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	if service.runs != nil {
		return nil
	}

	tasks := service.agentTasks.Tasks()

	submitter, err := assistant.NewAgentSubmitter(
		tasks,
		service.assistant.Agents,
	)
	if err != nil {
		return serviceError(err, "create workflow agent submitter")
	}

	controller, err := assistant.NewWorkflowController(
		submitter,
		tasks,
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

	service.runner = runner
	service.runs = runs

	return nil
}
