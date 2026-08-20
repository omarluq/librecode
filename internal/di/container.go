// Package di wires the application runtime dependency graph.
package di

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/taskruntime"
)

const (
	applicationContextKey = "application.context"
	startupCleanupTimeout = 10 * time.Second
)

// Container wraps the root injector used by the CLI runtime.
type Container struct {
	injector     *do.RootScope
	runtime      *RuntimeServices
	buildRuntime func(context.Context) (*RuntimeServices, error)
	lifecycle    sync.Mutex
	closed       bool
	started      bool
}

// RuntimeServices is the fully constructed and started application runtime.
type RuntimeServices struct {
	Config        *ConfigService       `do:""`
	Database      *DatabaseService     `do:""`
	Auth          *AuthService         `do:""`
	Models        *ModelService        `do:""`
	Extensions    *ExtensionService    `do:""`
	Assistant     *AssistantService    `do:""`
	AgentTasks    *AgentTaskService    `do:""`
	Workflows     *WorkflowService     `do:""`
	ChatWorkflows *ChatWorkflowService `do:""`
	TaskRuntime   *TaskRuntimeService  `do:""`
}

// NewContainer builds the root injector for the CLI runtime.
func NewContainer(ctx context.Context, configPath string, overrides ConfigOverrides) (*Container, error) {
	if ctx == nil {
		return nil, oops.In("di").Code("nil_application_context").Errorf("application context is required")
	}

	injector := do.New()
	do.ProvideNamedValue(injector, applicationContextKey, ctx)
	do.ProvideNamedValue(injector, ConfigPathKey, configPath)
	do.ProvideNamedValue(injector, ConfigOverridesKey, overrides)
	RegisterServices(injector)

	container := &Container{
		injector:     injector,
		runtime:      nil,
		buildRuntime: nil,
		closed:       false,
		started:      false,
		lifecycle:    sync.Mutex{},
	}
	container.buildRuntime = container.constructRuntime

	if _, err := do.Invoke[*ConfigService](injector); err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), startupCleanupTimeout)
		defer cancel()

		wrapped := oops.In("di").Code("container_init").Wrapf(err, "initialize container")

		return nil, joinShutdownError(wrapped, injector.ShutdownWithContext(shutdownCtx))
	}

	return container, nil
}

func applicationContext(injector do.Injector) (context.Context, error) {
	return do.InvokeNamed[context.Context](injector, applicationContextKey)
}

// ShutdownWithContext stops all registered services using the given context.
func (c *Container) ShutdownWithContext(ctx context.Context) *do.ShutdownReport {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()

	return c.shutdownLocked(ctx)
}

func (c *Container) shutdownLocked(ctx context.Context) *do.ShutdownReport {
	if c.closed {
		return &do.ShutdownReport{Succeed: true}
	}

	c.closed = true
	if c.runtime != nil {
		c.runtime.Assistant.capabilities.revoke()
	}

	return c.injector.ShutdownWithContext(ctx)
}

// StartRuntime constructs the complete runtime and starts its durable workers.
func (c *Container) StartRuntime() (*RuntimeServices, error) {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()

	if c.closed {
		return nil, oops.In("di").Code("container_closed").Errorf("start runtime services: container is closed")
	}

	if c.started {
		return c.runtime, nil
	}

	ctx, resolveErr := applicationContext(c.injector)
	if resolveErr != nil {
		return nil, c.runtimeStartErrorLocked(resolveErr)
	}

	if contextErr := ctx.Err(); contextErr != nil {
		return nil, c.runtimeStartErrorLocked(contextErr)
	}

	runtimeServices, err := c.buildRuntime(ctx)
	if err != nil {
		return nil, c.runtimeStartErrorLocked(err)
	}

	if contextErr := ctx.Err(); contextErr != nil {
		if runtimeServices != nil && runtimeServices.Assistant != nil {
			runtimeServices.Assistant.capabilities.revoke()
		}

		return nil, c.runtimeStartErrorLocked(contextErr)
	}

	c.runtime = runtimeServices
	c.started = true

	return runtimeServices, nil
}

func (c *Container) constructRuntime(ctx context.Context) (*RuntimeServices, error) {
	services, err := c.resolveRuntimeServices()
	if err != nil {
		return nil, err
	}

	startServices := []func(context.Context) error{
		services.AgentTasks.Start,
		services.Workflows.Start,
		services.ChatWorkflows.Start,
	}
	for _, start := range startServices {
		if err := start(ctx); err != nil {
			return nil, err
		}
	}

	agentTasks := services.AgentTasks.Tasks()
	if err := registerGenericCancellation(services, func(
		ctx context.Context, owner, taskID string,
	) (*database.TaskEntity, bool, error) {
		return agentTasks.Cancel(ctx, owner, taskID, database.CancelSourceParent)
	}); err != nil {
		return nil, err
	}

	if err := services.Assistant.capabilities.publish(
		agentTasks,
		services.ChatWorkflows.Dispatcher(),
	); err != nil {
		return nil, serviceError(err, "publish runtime capabilities")
	}

	published := true
	defer func() {
		if published {
			services.Assistant.capabilities.revoke()
		}
	}()

	startWorkers := []func(context.Context) error{
		services.TaskRuntime.Start,
		services.AgentTasks.StartWorkers,
		services.ChatWorkflows.StartWorkers,
	}
	for _, start := range startWorkers {
		if err := start(ctx); err != nil {
			return nil, err
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, oops.In("di").Code("runtime_start_canceled").Wrapf(err, "start runtime services")
	}

	published = false

	return services, nil
}

func registerGenericCancellation(services *RuntimeServices, agentCancel taskruntime.CancelFunc) error {
	manager := services.TaskRuntime.Manager
	if err := manager.RegisterCancel(database.TaskKindTool, func(
		ctx context.Context, owner, taskID string,
	) (*database.TaskEntity, bool, error) {
		entity, found, err := services.TaskRuntime.Tools.Cancel(ctx, owner, taskID)
		if err != nil {
			return nil, found, serviceError(err, "cancel tool task")
		}

		if entity == nil {
			return nil, found, nil
		}

		return &entity.Task, found, nil
	}); err != nil {
		return serviceError(err, "register tool task cancellation")
	}

	if err := manager.RegisterCancel(database.TaskKindAgent, agentCancel); err != nil {
		return serviceError(err, "register agent task cancellation")
	}

	workflowRuns := services.ChatWorkflows.Runs()

	if err := manager.RegisterCancel(database.TaskKindWorkflow, func(
		ctx context.Context, owner, taskID string,
	) (*database.TaskEntity, bool, error) {
		if _, err := workflowRuns.Cancel(ctx, owner, taskID); err != nil {
			return nil, false, serviceError(err, "cancel workflow task")
		}

		return manager.GetTask(ctx, owner, taskID)
	}); err != nil {
		return serviceError(err, "register workflow task cancellation")
	}

	return nil
}

func (c *Container) resolveRuntimeServices() (*RuntimeServices, error) {
	services, err := do.InvokeStruct[*RuntimeServices](c.injector)
	if err != nil {
		return nil, serviceError(err, "resolve runtime services")
	}

	return services, nil
}

func (c *Container) runtimeStartErrorLocked(startErr error) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), startupCleanupTimeout)
	defer cancel()

	wrapped := oops.In("di").Code("runtime_start").Wrapf(startErr, "start runtime services")

	return joinShutdownError(wrapped, c.shutdownLocked(shutdownCtx))
}

func joinShutdownError(base error, report *do.ShutdownReport) error {
	if report == nil || report.Succeed || report.Error() == "" {
		return base
	}

	return errors.Join(base, report)
}

func (c *Container) lockForResolution() error {
	c.lifecycle.Lock()

	if c.closed {
		c.lifecycle.Unlock()

		return oops.In("di").Code("container_closed").Errorf("resolve service: container is closed")
	}

	return nil
}

// ConfigService resolves the configuration service.
func (c *Container) ConfigService() (*ConfigService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*ConfigService](c.injector)
}

// AuthService resolves the auth service.
func (c *Container) AuthService() (*AuthService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*AuthService](c.injector)
}

// DatabaseService resolves the database service.
func (c *Container) DatabaseService() (*DatabaseService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*DatabaseService](c.injector)
}

// ExtensionService resolves the extension service.
func (c *Container) ExtensionService() (*ExtensionService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*ExtensionService](c.injector)
}

// ModelService resolves the model service.
func (c *Container) ModelService() (*ModelService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*ModelService](c.injector)
}

// AssistantService resolves the assistant runtime without starting workers.
func (c *Container) AssistantService() (*AssistantService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*AssistantService](c.injector)
}

// AgentTaskService resolves the inert durable background agent service.
func (c *Container) AgentTaskService() (*AgentTaskService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*AgentTaskService](c.injector)
}

// WorkflowService resolves inert script-driven durable agent orchestration.
func (c *Container) WorkflowService() (*WorkflowService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*WorkflowService](c.injector)
}

// ChatWorkflowService resolves the inert interactive workflow dispatcher.
func (c *Container) ChatWorkflowService() (*ChatWorkflowService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*ChatWorkflowService](c.injector)
}

// ToolService resolves the tool service.
func (c *Container) ToolService() (*ToolService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*ToolService](c.injector)
}

// SkillsService resolves the skills cache service.
func (c *Container) SkillsService() (*SkillsService, error) {
	if err := c.lockForResolution(); err != nil {
		return nil, err
	}
	defer c.lifecycle.Unlock()

	return do.Invoke[*SkillsService](c.injector)
}
