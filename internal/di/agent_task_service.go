package di

import (
	"context"
	"log/slog"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/agenttask"
)

// AgentTaskService owns durable background agent execution.
type AgentTaskService struct {
	Tasks *agenttask.Service
}

// NewAgentTaskService wires the assistant runtime into the durable task scheduler.
func NewAgentTaskService(injector do.Injector) (*AgentTaskService, error) {
	databaseService := do.MustInvoke[*DatabaseService](injector)
	assistantService := do.MustInvoke[*AssistantService](injector)

	var logger *slog.Logger
	if loggerService, loggerErr := do.Invoke[*LoggerService](injector); loggerErr == nil {
		logger = loggerService.SlogLogger
	}

	runner, err := agenttask.NewRuntimeRunner(
		assistantService.Runtime,
		assistantService.Agents,
		databaseService.Sessions,
	)
	if err != nil {
		return nil, serviceError(err, "create agent task runner")
	}

	tasks, err := agenttask.New(context.Background(), agenttask.Options{
		Tasks: databaseService.Tasks, AgentTasks: databaseService.AgentTasks,
		Runner: runner, Concurrency: 0, SessionConcurrency: 0, QueueCapacity: 0, Timeout: 0,
		Logger: logger,
	})
	if err != nil {
		return nil, serviceError(err, "create agent task service")
	}

	assistantService.Runtime.SetAgentTaskController(tasks)

	return &AgentTaskService{Tasks: tasks}, nil
}

// Shutdown stops workers before the database service is closed.
func (service *AgentTaskService) Shutdown(ctx context.Context) error {
	return serviceError(service.Tasks.Shutdown(ctx), "shutdown agent task service")
}
