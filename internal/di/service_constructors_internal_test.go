package di

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/agenttask"
	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/workflow"
)

func provideTestApplicationContext(injector do.Injector) {
	do.ProvideNamedValue(injector, applicationContextKey, context.Background())
}

func newIsolatedContainer(ctx context.Context, t *testing.T, overrides ConfigOverrides) *Container {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	databasePath := filepath.Join(t.TempDir(), "librecode.db")
	require.NoError(t, os.WriteFile(
		configPath,
		fmt.Appendf(nil, "database:\n  path: %q\n", databasePath),
		0o600,
	))

	container, err := NewContainer(ctx, configPath, overrides)
	require.NoError(t, err)

	return container
}

func TestNewCacheServiceUsesConfiguredCache(t *testing.T) {
	t.Parallel()

	injector := do.New()
	do.ProvideValue(injector, &ConfigService{cfg: testServiceConfig(), path: "", interactive: false})

	service, err := NewCacheService(injector)
	require.NoError(t, err)
	require.NotNil(t, service.Responses)
	t.Cleanup(service.Shutdown)
}

func TestNewToolServiceUsesCurrentWorkingDirectory(t *testing.T) {
	t.Parallel()

	service, err := NewToolService(do.New())
	require.NoError(t, err)
	require.NotNil(t, service.Registry)

	cwd, err := filepath.Abs(".")
	require.NoError(t, err)
	assert.Equal(t, cwd, service.Registry.CWD())
}

func testServiceConfig() *config.Config {
	cfg := config.Load("").MustGet()
	cfg.Cache.Enabled = true
	cfg.Cache.Capacity = 2
	cfg.Cache.TTL = time.Minute

	return cfg
}

func TestNewTaskRuntimeServiceWiringBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*DatabaseService, *ToolService)
		wantCode  string
	}{
		{
			name: "missing task repository",
			configure: func(databaseService *DatabaseService, _ *ToolService) {
				databaseService.Tasks = nil
			},
			wantCode: "missing_task_repositories",
		},
		{
			name: "missing tool task repository",
			configure: func(databaseService *DatabaseService, _ *ToolService) {
				databaseService.ToolTasks = nil
			},
			wantCode: "missing_task_repositories",
		},
		{
			name: "missing coordinator",
			configure: func(_ *DatabaseService, toolService *ToolService) {
				toolService.Coordinator = nil
			},
			wantCode: "missing_tool_coordinator",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			injector := do.New()
			databaseService := newTestDatabaseService(t)
			toolService, err := NewToolService(injector)
			require.NoError(t, err)
			testCase.configure(databaseService, toolService)
			provideTaskRuntimeDependencies(injector, databaseService, toolService)

			service, err := NewTaskRuntimeService(injector)
			assert.Nil(t, service)
			requireOopsCode(t, err, testCase.wantCode)
		})
	}
}

func TestNewTaskRuntimeServiceConstructsSchedulerManagerAndTools(t *testing.T) {
	t.Parallel()

	injector := do.New()
	databaseService := newTestDatabaseService(t)
	toolService, err := NewToolService(injector)
	require.NoError(t, err)
	provideTaskRuntimeDependencies(injector, databaseService, toolService)

	service, err := NewTaskRuntimeService(injector)
	require.NoError(t, err)
	require.NotNil(t, service)
	t.Cleanup(func() { assert.NoError(t, service.Shutdown(context.Background())) })
	assert.NotNil(t, service.Runtime)
	assert.NotNil(t, service.Manager)
	assert.NotNil(t, service.Tools)
	require.NoError(t, service.Start(t.Context()))
}

func provideTaskRuntimeDependencies(
	injector do.Injector,
	databaseService *DatabaseService,
	toolService *ToolService,
) {
	logger := zerolog.Nop()

	do.ProvideValue(injector, &ConfigService{cfg: testServiceConfig(), path: "", interactive: false})
	do.ProvideValue(injector, databaseService)
	do.ProvideValue(injector, &LoggerService{
		SlogLogger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		ZerologLogger: logger,
	})
	do.ProvideValue(injector, toolService)
}

func TestNewAgentTaskServiceRejectsIncompleteWiring(t *testing.T) {
	t.Parallel()

	injector := do.New()
	provideTestApplicationContext(injector)
	do.ProvideValue(injector, &ConfigService{cfg: testServiceConfig(), path: "", interactive: false})
	do.ProvideValue(injector, &DatabaseService{
		DB: nil, Sessions: nil, Documents: nil, Tasks: nil, AgentTasks: nil, Workflows: nil, ToolTasks: nil,
		path: "",
	})
	do.ProvideValue(injector, &AssistantService{
		Runtime: nil, Agents: nil, capabilities: nil,
	})

	do.ProvideValue(injector, &LoggerService{SlogLogger: nil, ZerologLogger: zerolog.Logger{}})

	service, err := NewAgentTaskService(injector)
	require.ErrorContains(t, err, "create agent task runner")
	assert.Nil(t, service)
}

func TestNewAgentTaskServiceWrapsSchedulerError(t *testing.T) {
	t.Parallel()

	injector := do.New()
	provideTestApplicationContext(injector)

	databaseService := newTestDatabaseService(t)
	databaseService.Tasks = nil
	do.ProvideValue(injector, databaseService)
	do.ProvideValue(injector, newTestAssistantService(t, injector))

	service, err := NewAgentTaskService(injector)
	require.NoError(t, err)

	err = service.Start(t.Context())
	require.ErrorContains(t, err, "create agent task service")
}

func TestWorkflowServiceRejectsUnconstructedAgentTaskService(t *testing.T) {
	t.Parallel()

	service := &WorkflowService{
		runner: nil, runs: nil, database: nil, assistant: nil, agentTasks: new(AgentTaskService),
		lifecycle: sync.Mutex{},
	}
	requireOopsCode(t, service.Start(t.Context()), "agent_task_service_not_constructed")
}

func TestAgentTaskServiceWiresAndShutsDown(t *testing.T) {
	t.Parallel()

	injector := do.New()
	provideTestApplicationContext(injector)
	do.ProvideValue(injector, newTestDatabaseService(t))
	assistantService := newTestAssistantService(t, injector)
	do.ProvideValue(injector, assistantService)

	service, err := NewAgentTaskService(injector)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, service.Shutdown(context.Background())) })
	require.Nil(t, service.Tasks())
	require.NoError(t, service.Start(t.Context()))
	require.NotNil(t, service.Tasks())
}

func TestAgentTaskServicePropagatesConfiguredWorkerBounds(t *testing.T) {
	t.Parallel()

	cfg := testServiceConfig()
	cfg.Tasks.Workers = 15
	cfg.Tasks.SessionWorkers = 7

	injector := do.New()
	provideTestApplicationContext(injector)
	do.ProvideValue(injector, &ConfigService{cfg: cfg, path: "", interactive: false})

	do.ProvideValue(injector, newTestDatabaseService(t))
	assistantService := newTestAssistantServiceWithoutConfig(t, injector)
	do.ProvideValue(injector, assistantService)

	service, err := NewAgentTaskService(injector)
	require.NoError(t, err)

	assert.Equal(t, 15, service.options.Concurrency)
	assert.Equal(t, 7, service.options.SessionConcurrency)
}

func newTestAssistantService(t *testing.T, injector do.Injector) *AssistantService {
	t.Helper()

	provideTestAssistantDependencies(t, injector)
	service, err := NewAssistantService(injector)
	require.NoError(t, err)

	return service
}

func newTestAssistantServiceWithoutConfig(t *testing.T, injector do.Injector) *AssistantService {
	t.Helper()

	// The caller already provided a ConfigService with a customized config;
	// reusing provideTestAssistantDependencies would re-provide it and panic.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provideTestAssistantDependenciesExceptConfig(t, injector, logger)

	service, err := NewAssistantService(injector)
	require.NoError(t, err)

	return service
}

func TestNewContainerRejectsNilContext(t *testing.T) {
	t.Parallel()

	var ctx context.Context

	container, err := NewContainer(ctx, "", ConfigOverrides{DisableExtensions: false, Interactive: false})
	assert.Nil(t, container)
	requireOopsCode(t, err, "nil_application_context")
}

func TestContainerAccessorReturnsConstructionError(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-file")
	require.NoError(t, os.WriteFile(homeFile, []byte("not a directory"), 0o600))
	t.Setenv("LIBRECODE_HOME", homeFile)

	container, err := NewContainer(context.Background(), "", ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.Empty(t, report.Errors)
	})

	service, err := container.DatabaseService()
	require.Error(t, err)
	assert.Nil(t, service)
	assert.Contains(t, err.Error(), "create database dir")
}

func TestStartRuntimeRejectsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	container := newIsolatedContainer(ctx, t, ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})

	services, err := container.StartRuntime()
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, services)
}

func TestAssistantServiceAccessorDoesNotStartWorkers(t *testing.T) {
	t.Parallel()

	container := newIsolatedContainer(context.Background(), t, ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.Empty(t, report.Errors)
	})

	assistantService, err := container.AssistantService()
	require.NoError(t, err)

	assertRuntimeCapabilitiesUnavailable(t, assistantService)

	agentTasks, err := container.AgentTaskService()
	require.NoError(t, err)
	assert.Nil(t, agentTasks.Tasks())

	workflows, err := container.WorkflowService()
	require.NoError(t, err)
	assert.Nil(t, workflows.Runs())

	chatWorkflows, err := container.ChatWorkflowService()
	require.NoError(t, err)
	assert.Nil(t, chatWorkflows.Dispatcher())
}

func TestStartRuntimeCleansUpPartialConstruction(t *testing.T) {
	t.Parallel()

	container := newIsolatedContainer(context.Background(), t, ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})

	recorder := new(shutdownRecorder)
	do.ProvideValue(container.injector, recorder)
	do.Provide(container.injector, newBootstrapParent)
	do.Provide(container.injector, newBootstrapChild)

	expectedErr := errors.New("runtime construction failed")
	container.buildRuntime = func(context.Context) (*RuntimeServices, error) {
		if _, resolveErr := do.Invoke[*bootstrapChild](container.injector); resolveErr != nil {
			return nil, resolveErr
		}

		return nil, expectedErr
	}

	assistantService, err := container.AssistantService()
	require.NoError(t, err)
	assertRuntimeCapabilitiesUnavailable(t, assistantService)

	runtimeServices, err := container.StartRuntime()
	require.ErrorIs(t, err, expectedErr)
	assert.Nil(t, runtimeServices)
	assert.Equal(t, []string{"child", "parent"}, recorder.snapshot())
	assertRuntimeCapabilitiesUnavailable(t, assistantService)

	_, err = container.DatabaseService()
	assert.Error(t, err)
}

// shutdownRecorder records lifecycle order without relying on timing or goroutine counts.
type shutdownRecorder struct {
	events []string
	mu     sync.Mutex
}

func (recorder *shutdownRecorder) append(event string) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	recorder.events = append(recorder.events, event)
}

func (recorder *shutdownRecorder) snapshot() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	return slices.Clone(recorder.events)
}

type bootstrapParent struct {
	recorder *shutdownRecorder
}

func newBootstrapParent(injector do.Injector) (*bootstrapParent, error) {
	recorder, err := do.Invoke[*shutdownRecorder](injector)
	if err != nil {
		return nil, err
	}

	return &bootstrapParent{recorder: recorder}, nil
}

func (resource *bootstrapParent) Shutdown(context.Context) error {
	resource.recorder.append("parent")

	return nil
}

type bootstrapChild struct {
	recorder *shutdownRecorder
}

func newBootstrapChild(injector do.Injector) (*bootstrapChild, error) {
	parent, err := do.Invoke[*bootstrapParent](injector)
	if err != nil {
		return nil, err
	}

	return &bootstrapChild{recorder: parent.recorder}, nil
}

func (resource *bootstrapChild) Shutdown(context.Context) error {
	resource.recorder.append("child")

	return nil
}

func TestStartRuntimeRevokesCapabilitiesAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	container := newIsolatedContainer(ctx, t, ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})

	assistantService, err := container.AssistantService()
	require.NoError(t, err)

	container.buildRuntime = func(context.Context) (*RuntimeServices, error) {
		require.NoError(t, assistantService.capabilities.publish(
			new(agenttask.Service),
			new(workflow.Dispatcher),
		))
		cancel()

		return &RuntimeServices{
			Config:        nil,
			Database:      nil,
			Auth:          nil,
			Models:        nil,
			Extensions:    nil,
			Assistant:     assistantService,
			AgentTasks:    nil,
			Workflows:     nil,
			ChatWorkflows: nil,
			TaskRuntime:   nil,
		}, nil
	}

	runtimeServices, err := container.StartRuntime()
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, runtimeServices)
	assertRuntimeCapabilitiesUnavailable(t, assistantService)

	_, err = container.ConfigService()
	requireOopsCode(t, err, "container_closed")
}

func TestRuntimeCapabilitiesPublicationLifecycle(t *testing.T) {
	t.Parallel()

	capabilities := newRuntimeCapabilities()
	agentTasks := new(agenttask.Service)
	workflows := new(workflow.Dispatcher)

	assertRuntimeCapabilitySetUnavailable(t, capabilities)
	require.NoError(t, capabilities.publish(agentTasks, workflows))

	published, err := capabilities.load()
	require.NoError(t, err)
	assert.Same(t, agentTasks, published.agentTasks)
	assert.Same(t, workflows, published.workflows)

	requireOopsCode(t, capabilities.publish(agentTasks, workflows), "runtime_capabilities_published")

	capabilities.revoke()
	assertRuntimeCapabilitySetUnavailable(t, capabilities)
}

func TestRuntimeCapabilitiesRejectIncompleteSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		agentTasks assistant.AgentTaskController
		workflows  assistant.WorkflowSubmitter
		wantCode   string
	}{
		{
			name: "agent tasks", agentTasks: nil, workflows: new(workflow.Dispatcher),
			wantCode: "nil_agent_task_controller",
		},
		{name: "workflows", agentTasks: new(agenttask.Service), workflows: nil, wantCode: "nil_workflow_submitter"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			capabilities := newRuntimeCapabilities()
			requireOopsCode(t, capabilities.publish(test.agentTasks, test.workflows), test.wantCode)
			assertRuntimeCapabilitySetUnavailable(t, capabilities)
		})
	}
}

func assertRuntimeCapabilitySetUnavailable(t *testing.T, capabilities *runtimeCapabilities) {
	t.Helper()

	_, err := capabilities.List(t.Context(), "missing", 1)
	requireOopsCode(t, err, "runtime_capabilities_unavailable")

	_, err = capabilities.Submit(t.Context(), nil)
	requireOopsCode(t, err, "runtime_capabilities_unavailable")
}

func requireOopsCode(t *testing.T, err error, want string) {
	t.Helper()
	require.Error(t, err)

	var coded oops.OopsError
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, want, coded.Code())
}

func TestStartRuntimeStartsWorkersExplicitly(t *testing.T) {
	t.Parallel()

	container := newIsolatedContainer(context.Background(), t, ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.Empty(t, report.Errors)
	})

	runtimeServices, err := container.StartRuntime()
	require.NoError(t, err)
	require.NotNil(t, runtimeServices)

	repeatedServices, err := container.StartRuntime()
	require.NoError(t, err)
	assert.Same(t, runtimeServices, repeatedServices)

	agentTasks, err := container.AgentTaskService()
	require.NoError(t, err)
	require.NotNil(t, agentTasks.Tasks())

	workflows, err := container.WorkflowService()
	require.NoError(t, err)
	require.NotNil(t, workflows.Runs())

	chatWorkflows, err := container.ChatWorkflowService()
	require.NoError(t, err)
	require.NotNil(t, chatWorkflows.Dispatcher())

	tasks, err := runtimeServices.Assistant.Runtime.AgentTasks(t.Context(), "missing", 1)
	require.NoError(t, err)
	assert.Empty(t, tasks)

	_, found, err := runtimeServices.Assistant.Runtime.AgentTask(t.Context(), "missing")
	require.NoError(t, err)
	assert.False(t, found)

	published := runtimeServices.Assistant.capabilities.set.Load()
	require.NotNil(t, published)
	assert.Same(t, runtimeServices.AgentTasks.Tasks(), published.agentTasks)
	assert.Same(t, runtimeServices.ChatWorkflows.Dispatcher(), published.workflows)

	report := container.ShutdownWithContext(t.Context())
	require.Empty(t, report.Errors)
	assertRuntimeCapabilitiesUnavailable(t, runtimeServices.Assistant)
}

func assertRuntimeCapabilitiesUnavailable(t *testing.T, service *AssistantService) {
	t.Helper()

	tasks, err := service.Runtime.AgentTasks(t.Context(), "missing", 1)
	requireOopsCode(t, err, "runtime_capabilities_unavailable")
	assert.Nil(t, tasks)

	_, err = service.capabilities.Submit(t.Context(), nil)
	requireOopsCode(t, err, "runtime_capabilities_unavailable")
}

func TestContainerRejectsResolutionAfterShutdown(t *testing.T) {
	t.Parallel()

	container := newIsolatedContainer(context.Background(), t, ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	require.True(t, container.ShutdownWithContext(context.Background()).Succeed)
	require.True(t, container.ShutdownWithContext(context.Background()).Succeed)

	runtimeServices, err := container.StartRuntime()
	assert.Nil(t, runtimeServices)
	requireOopsCode(t, err, "container_closed")

	for _, service := range containerServiceAccessors(container) {
		t.Run(service.name, func(t *testing.T) {
			t.Parallel()

			resolved, resolveErr := service.resolve()
			assert.Nil(t, resolved)
			requireOopsCode(t, resolveErr, "container_closed")
		})
	}
}

func TestContainerServiceAccessors(t *testing.T) {
	t.Parallel()

	container := newIsolatedContainer(context.Background(), t, ConfigOverrides{
		DisableExtensions: false,
		Interactive:       false,
	})
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.Empty(t, report.Errors)
	})

	for _, service := range containerServiceAccessors(container) {
		t.Run(service.name, func(t *testing.T) {
			t.Parallel()

			resolved, resolveErr := service.resolve()
			require.NoError(t, resolveErr)
			require.NotNil(t, resolved)
		})
	}
}

type containerServiceAccessor struct {
	resolve func() (any, error)
	name    string
}

func containerServiceAccessors(container *Container) []containerServiceAccessor {
	return []containerServiceAccessor{
		{name: "config", resolve: func() (any, error) { return container.ConfigService() }},
		{name: "auth", resolve: func() (any, error) { return container.AuthService() }},
		{name: "database", resolve: func() (any, error) { return container.DatabaseService() }},
		{name: "extension", resolve: func() (any, error) { return container.ExtensionService() }},
		{name: "model", resolve: func() (any, error) { return container.ModelService() }},
		{name: "assistant", resolve: func() (any, error) { return container.AssistantService() }},
		{name: "agent task", resolve: func() (any, error) { return container.AgentTaskService() }},
		{name: "workflow", resolve: func() (any, error) { return container.WorkflowService() }},
		{name: "chat workflow", resolve: func() (any, error) { return container.ChatWorkflowService() }},
		{name: "tool", resolve: func() (any, error) { return container.ToolService() }},
		{name: "skills", resolve: func() (any, error) { return container.SkillsService() }},
	}
}
