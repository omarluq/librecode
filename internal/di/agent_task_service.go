package di

import (
	"context"
	"sync"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/agenttask"
)

// AgentTaskService owns durable background agent execution.
type AgentTaskService struct {
	tasks     *agenttask.Service
	options   *agenttask.Options
	lifecycle sync.Mutex
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
		tasks: nil,
		options: &agenttask.Options{
			Tasks: databaseService.Tasks, AgentTasks: databaseService.AgentTasks, Workflows: databaseService.Workflows,
			Runner: runner, Concurrency: 0, SessionConcurrency: 0, QueueCapacity: 0, Timeout: 0,
			Logger: logger,
		},
		lifecycle: sync.Mutex{},
	}, nil
}

// Tasks returns the constructed durable task scheduler, if startup reached that stage.
func (service *AgentTaskService) Tasks() *agenttask.Service {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	return service.tasks
}

// Start constructs the durable task scheduler without starting workers.
func (service *AgentTaskService) Start(ctx context.Context) error {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	if service.tasks != nil {
		return nil
	}

	tasks, err := agenttask.NewStopped(ctx, service.options)
	if err != nil {
		return serviceError(err, "create agent task service")
	}

	service.tasks = tasks

	return nil
}

// StartWorkers starts scheduler workers after runtime capabilities are published.
func (service *AgentTaskService) StartWorkers(ctx context.Context) error {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	if service.tasks == nil {
		return oops.In("di").Code("agent_task_service_not_constructed").
			Errorf("agent task service is not constructed")
	}

	return serviceError(service.tasks.Start(ctx), "start agent task workers")
}

// Shutdown stops workers before the database service is closed.
func (service *AgentTaskService) Shutdown(ctx context.Context) error {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	if service.tasks == nil {
		return nil
	}

	return serviceError(service.tasks.Shutdown(ctx), "shutdown agent task service")
}
