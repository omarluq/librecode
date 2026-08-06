package di

import (
	"context"
	"log/slog"
	"sync"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/workflow"
)

// ChatWorkflowService owns the in-process workflow dispatcher used by interactive chat.
type ChatWorkflowService struct {
	runs       *workflow.Service
	dispatcher *workflow.Dispatcher
	workflows  *WorkflowService
	database   *DatabaseService
	logger     *slog.Logger
	lifecycle  sync.Mutex
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

	loggerService, err := do.Invoke[*LoggerService](injector)
	if err != nil {
		return nil, serviceError(err, "resolve logger service")
	}

	return &ChatWorkflowService{
		runs: nil, dispatcher: nil, workflows: workflows, database: databaseService,
		logger: loggerService.SlogLogger, lifecycle: sync.Mutex{},
	}, nil
}

// Runs returns the durable workflow service, if startup reached that stage.
func (service *ChatWorkflowService) Runs() *workflow.Service {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	return service.runs
}

// Dispatcher returns the constructed workflow dispatcher, if startup reached that stage.
func (service *ChatWorkflowService) Dispatcher() *workflow.Dispatcher {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	return service.dispatcher
}

// Start constructs the in-process workflow dispatcher without starting workers.
func (service *ChatWorkflowService) Start(ctx context.Context) error {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	if service.dispatcher != nil {
		return nil
	}

	runs := service.workflows.Runs()

	dispatcher, err := workflow.NewStoppedDispatcher(ctx, workflow.DispatcherOptions{
		Service: runs, Tasks: service.database.Tasks, Logger: service.logger,
		Concurrency: 0, Buffer: 0, Interval: 0,
	})
	if err != nil {
		return serviceError(err, "create chat workflow dispatcher")
	}

	service.runs = runs
	service.dispatcher = dispatcher

	return nil
}

// StartWorkers starts workflow workers after runtime capabilities are published.
func (service *ChatWorkflowService) StartWorkers() error {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	if service.dispatcher == nil {
		return oops.In("di").Code("workflow_dispatcher_not_constructed").
			Errorf("workflow dispatcher is not constructed")
	}

	return serviceError(service.dispatcher.Start(), "start chat workflow dispatcher")
}

// Shutdown stops workflow workers before their dependencies are closed.
func (service *ChatWorkflowService) Shutdown(ctx context.Context) error {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	if service.dispatcher == nil {
		return nil
	}

	return serviceError(service.dispatcher.Shutdown(ctx), "shutdown chat workflow dispatcher")
}
