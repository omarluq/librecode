// Package di wires the application runtime dependency graph.
package di

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/samber/do/v2"
	"github.com/samber/oops"
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
	Config        *ConfigService
	Database      *DatabaseService
	Auth          *AuthService
	Models        *ModelService
	Extensions    *ExtensionService
	Assistant     *AssistantService
	AgentTasks    *AgentTaskService
	Workflows     *WorkflowService
	ChatWorkflows *ChatWorkflowService
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
		lifecycle:    sync.Mutex{},
		closed:       false,
		started:      false,
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

	if err := services.AgentTasks.Start(ctx); err != nil {
		return nil, err
	}

	if err := services.Workflows.Start(ctx); err != nil {
		return nil, err
	}

	if err := services.ChatWorkflows.Start(ctx); err != nil {
		return nil, err
	}

	return services, nil
}

func (c *Container) resolveRuntimeServices() (*RuntimeServices, error) {
	configService, err := do.Invoke[*ConfigService](c.injector)
	if err != nil {
		return nil, err
	}

	databaseService, err := do.Invoke[*DatabaseService](c.injector)
	if err != nil {
		return nil, err
	}

	authService, err := do.Invoke[*AuthService](c.injector)
	if err != nil {
		return nil, err
	}

	modelService, err := do.Invoke[*ModelService](c.injector)
	if err != nil {
		return nil, err
	}

	extensionService, err := do.Invoke[*ExtensionService](c.injector)
	if err != nil {
		return nil, err
	}

	assistantService, err := do.Invoke[*AssistantService](c.injector)
	if err != nil {
		return nil, err
	}

	agentTasks, err := do.Invoke[*AgentTaskService](c.injector)
	if err != nil {
		return nil, err
	}

	workflows, err := do.Invoke[*WorkflowService](c.injector)
	if err != nil {
		return nil, err
	}

	chatWorkflows, err := do.Invoke[*ChatWorkflowService](c.injector)
	if err != nil {
		return nil, err
	}

	return &RuntimeServices{
		Config:        configService,
		Database:      databaseService,
		Auth:          authService,
		Models:        modelService,
		Extensions:    extensionService,
		Assistant:     assistantService,
		AgentTasks:    agentTasks,
		Workflows:     workflows,
		ChatWorkflows: chatWorkflows,
	}, nil
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
