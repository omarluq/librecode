package di

import (
	"context"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/agenttask"
)

// AgentTaskService owns durable background agent execution.
type AgentTaskService struct {
	Tasks     *agenttask.Service
	assistant *AssistantService
	options   *agenttask.Options
}

// NewAgentTaskService wires the assistant runtime into the durable task scheduler.
func NewAgentTaskService(injector do.Injector) (*AgentTaskService, error) {
	databaseService, err := do.Invoke[*DatabaseService](injector)
	if err != nil {
		return nil, serviceError(err, "resolve database service")
	}

	assistantService, err := do.Invoke[*AssistantService](injector)
	if err != nil {
		return nil, serviceError(err, "resolve assistant service")
	}

	loggerService, err := do.Invoke[*LoggerService](injector)
	if err != nil {
		return nil, serviceError(err, "resolve logger service")
	}

	logger := loggerService.SlogLogger

	runner, err := agenttask.NewRuntimeRunner(
		assistantService.Runtime,
		assistantService.Agents,
		databaseService.Sessions,
	)
	if err != nil {
		return nil, serviceError(err, "create agent task runner")
	}

	return &AgentTaskService{
		Tasks:     nil,
		assistant: assistantService,
		options: &agenttask.Options{
			Tasks: databaseService.Tasks, AgentTasks: databaseService.AgentTasks, Workflows: databaseService.Workflows,
			Runner: runner, Concurrency: 0, SessionConcurrency: 0, QueueCapacity: 0, Timeout: 0,
			Logger: logger,
		},
	}, nil
}

// Start recovers durable tasks and starts scheduler workers.
func (service *AgentTaskService) Start(ctx context.Context) error {
	if service.Tasks != nil {
		return nil
	}

	tasks, err := agenttask.New(ctx, service.options)
	if err != nil {
		return serviceError(err, "create agent task service")
	}

	service.Tasks = tasks
	service.assistant.Runtime.SetAgentTaskController(tasks)

	return nil
}

// Shutdown stops workers before the database service is closed.
func (service *AgentTaskService) Shutdown(ctx context.Context) error {
	if service.Tasks == nil {
		return nil
	}

	return serviceError(service.Tasks.Shutdown(ctx), "shutdown agent task service")
}
