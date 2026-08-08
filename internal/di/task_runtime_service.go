package di

import (
	"context"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/taskruntime"
	"github.com/omarluq/librecode/internal/tooltask"
)

// TaskRuntimeService owns generic tool-task scheduling and admission.
type TaskRuntimeService struct {
	Runtime *taskruntime.Service
	Manager *taskruntime.Manager
	Tools   *tooltask.Service
}

type taskRuntimeDependencies struct {
	Config   *ConfigService   `do:""`
	Database *DatabaseService `do:""`
	Logger   *LoggerService   `do:""`
	Tools    *ToolService     `do:""`
}

// NewTaskRuntimeService wires durable tool-task admission and scheduling.
func NewTaskRuntimeService(injector do.Injector) (*TaskRuntimeService, error) {
	dependencies, err := do.InvokeStruct[taskRuntimeDependencies](injector)
	if err != nil {
		return nil, serviceError(err, "resolve task runtime dependencies")
	}

	if dependencies.Database.Tasks == nil || dependencies.Database.ToolTasks == nil {
		return nil, oops.In("di").Code("missing_task_repositories").
			Errorf("create task runtime: task repositories are required")
	}

	if dependencies.Tools.Coordinator == nil {
		return nil, oops.In("di").Code("missing_tool_coordinator").
			Errorf("create task runtime: tool coordinator is required")
	}

	cfg := dependencies.Config.Get().Tasks
	tools := tooltask.New(
		dependencies.Database.ToolTasks,
		dependencies.Tools.Coordinator,
		cfg.DefaultTimeout,
		cfg.MaxTimeout,
		cfg.MaxOutcomeBytes,
	)

	runtime, err := taskruntime.New(taskruntime.Options{
		Tasks:             dependencies.Database.Tasks,
		Logger:            dependencies.Logger.SlogLogger,
		Workers:           cfg.Workers,
		PollInterval:      cfg.PollInterval,
		LeaseDuration:     cfg.LeaseDuration,
		HeartbeatInterval: cfg.Heartbeat,
		RecoveryInterval:  cfg.RecoveryInterval,
		DefaultTimeout:    cfg.MaxTimeout,
		MaxPayloadBytes:   cfg.MaxOutcomeBytes,
	}, tools)
	if err != nil {
		return nil, oops.In("di").Code("create_task_runtime").Wrapf(err, "create task runtime")
	}

	tools.AttachRuntime(runtime)

	return &TaskRuntimeService{
		Runtime: runtime, Manager: taskruntime.NewManager(dependencies.Database.Tasks), Tools: tools,
	}, nil
}

// Start begins managed task polling and execution.
func (service *TaskRuntimeService) Start(ctx context.Context) error {
	if err := service.Runtime.Start(ctx); err != nil {
		return oops.In("di").Code("start_task_runtime").Wrapf(err, "start task runtime")
	}

	return nil
}

// Shutdown stops and drains managed task workers.
func (service *TaskRuntimeService) Shutdown(ctx context.Context) error {
	if err := service.Runtime.Shutdown(ctx); err != nil {
		return oops.In("di").Code("shutdown_task_runtime").Wrapf(err, "shutdown task runtime")
	}

	return nil
}
