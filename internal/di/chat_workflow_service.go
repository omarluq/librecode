package di

import (
	"context"
	"log/slog"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/workflow"
)

// ChatWorkflowService owns the in-process workflow dispatcher used by interactive chat.
type ChatWorkflowService struct {
	Runs       *workflow.Service
	Dispatcher *workflow.Dispatcher
	workflows  *WorkflowService
	database   *DatabaseService
	assistant  *AssistantService
	logger     *slog.Logger
}

// NewChatWorkflowService enables model-authored workflows for interactive chat.
func NewChatWorkflowService(injector do.Injector) (*ChatWorkflowService, error) {
	workflows, err := do.Invoke[*WorkflowService](injector)
	if err != nil {
		return nil, serviceError(err, "resolve workflow service")
	}

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

	return &ChatWorkflowService{
		Runs: nil, Dispatcher: nil, workflows: workflows, database: databaseService,
		assistant: assistantService, logger: loggerService.SlogLogger,
	}, nil
}

// Start starts the in-process workflow dispatcher.
func (service *ChatWorkflowService) Start(ctx context.Context) error {
	if service.Dispatcher != nil {
		return nil
	}

	dispatcher, err := workflow.NewDispatcher(ctx, workflow.DispatcherOptions{
		Service: service.workflows.Runs, Tasks: service.database.Tasks, Logger: service.logger,
		Concurrency: 0, Buffer: 0, Interval: 0,
	})
	if err != nil {
		return serviceError(err, "create chat workflow dispatcher")
	}

	service.Runs = service.workflows.Runs
	service.Dispatcher = dispatcher
	service.assistant.Runtime.SetWorkflowSubmitter(dispatcher)

	return nil
}

// Shutdown stops workflow workers before their dependencies are closed.
func (service *ChatWorkflowService) Shutdown(ctx context.Context) error {
	if service.Dispatcher == nil {
		return nil
	}

	return serviceError(service.Dispatcher.Shutdown(ctx), "shutdown chat workflow dispatcher")
}
