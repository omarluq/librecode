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
}

// NewChatWorkflowService enables model-authored workflows for interactive chat.
func NewChatWorkflowService(injector do.Injector) (*ChatWorkflowService, error) {
	workflows := do.MustInvoke[*WorkflowService](injector)
	databaseService := do.MustInvoke[*DatabaseService](injector)
	assistantService := do.MustInvoke[*AssistantService](injector)

	var logger *slog.Logger
	if loggerService, err := do.Invoke[*LoggerService](injector); err == nil {
		logger = loggerService.SlogLogger
	}

	dispatcher, err := workflow.NewDispatcher(context.Background(), workflow.DispatcherOptions{
		Service: workflows.Runs, Tasks: databaseService.Tasks, Logger: logger,
		Concurrency: 0, Buffer: 0, Interval: 0,
	})
	if err != nil {
		return nil, serviceError(err, "create chat workflow dispatcher")
	}

	assistantService.Runtime.SetWorkflowSubmitter(dispatcher)

	return &ChatWorkflowService{Runs: workflows.Runs, Dispatcher: dispatcher}, nil
}

// Shutdown stops workflow workers before their dependencies are closed.
func (service *ChatWorkflowService) Shutdown(ctx context.Context) error {
	return serviceError(service.Dispatcher.Shutdown(ctx), "shutdown chat workflow dispatcher")
}
