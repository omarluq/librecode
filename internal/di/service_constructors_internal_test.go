package di

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
)

func provideTestApplicationContext(injector do.Injector) {
	do.ProvideNamedValue(injector, applicationContextKey, context.Background())
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

func TestNewAgentTaskServiceRejectsIncompleteWiring(t *testing.T) {
	t.Parallel()

	injector := do.New()
	provideTestApplicationContext(injector)
	do.ProvideValue(injector, &DatabaseService{
		DB: nil, Sessions: nil, Documents: nil, Tasks: nil, AgentTasks: nil, Workflows: nil, path: "",
	})
	do.ProvideValue(injector, &AssistantService{Runtime: nil, Agents: nil})

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

func TestAgentTaskServiceWiresAndShutsDown(t *testing.T) {
	t.Parallel()

	injector := do.New()
	provideTestApplicationContext(injector)
	do.ProvideValue(injector, newTestDatabaseService(t))
	assistant := newTestAssistantService(t, injector)
	do.ProvideValue(injector, assistant)

	service, err := NewAgentTaskService(injector)
	require.NoError(t, err)
	require.Nil(t, service.Tasks)
	require.NoError(t, service.Start(t.Context()))
	require.NotNil(t, service.Tasks)
	require.NoError(t, service.Shutdown(context.Background()))
}

func newTestAssistantService(t *testing.T, injector do.Injector) *AssistantService {
	t.Helper()

	provideTestAssistantDependencies(t, injector)
	service, err := NewAssistantService(injector)
	require.NoError(t, err)

	return service
}

func TestNewContainerRejectsNilContext(t *testing.T) {
	t.Parallel()

	var ctx context.Context

	container, err := NewContainer(ctx, "", ConfigOverrides{DisableExtensions: false, Interactive: false})
	assert.Nil(t, container)
	assert.ErrorContains(t, err, "application context is required")
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

	container, err := NewContainer(ctx, "", ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	require.NoError(t, err)

	services, err := container.StartRuntime()
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, services)
}

func TestAssistantServiceAccessorDoesNotStartWorkers(t *testing.T) {
	t.Parallel()

	container, err := NewContainer(context.Background(), "", ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.Empty(t, report.Errors)
	})

	_, err = container.AssistantService()
	require.NoError(t, err)

	agentTasks, err := container.AgentTaskService()
	require.NoError(t, err)
	assert.Nil(t, agentTasks.Tasks)

	workflows, err := container.WorkflowService()
	require.NoError(t, err)
	assert.Nil(t, workflows.Runs)

	chatWorkflows, err := container.ChatWorkflowService()
	require.NoError(t, err)
	assert.Nil(t, chatWorkflows.Dispatcher)
}

func TestStartRuntimeCleansUpPartialConstruction(t *testing.T) {
	t.Parallel()

	container, err := NewContainer(context.Background(), "", ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	require.NoError(t, err)

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

	runtimeServices, err := container.StartRuntime()
	require.ErrorIs(t, err, expectedErr)
	assert.Nil(t, runtimeServices)
	assert.Equal(t, []string{"child", "parent"}, recorder.snapshot())

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

func TestStartRuntimeDoesNotPublishAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	container, err := NewContainer(ctx, "", ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	require.NoError(t, err)

	container.buildRuntime = func(context.Context) (*RuntimeServices, error) {
		cancel()

		return new(RuntimeServices), nil
	}

	runtimeServices, err := container.StartRuntime()
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, runtimeServices)

	_, err = container.ConfigService()
	assert.Error(t, err)
}

func TestStartRuntimeStartsWorkersExplicitly(t *testing.T) {
	t.Parallel()

	container, err := NewContainer(context.Background(), "", ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	require.NoError(t, err)
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
	require.NotNil(t, agentTasks.Tasks)

	workflows, err := container.WorkflowService()
	require.NoError(t, err)
	require.NotNil(t, workflows.Runs)

	chatWorkflows, err := container.ChatWorkflowService()
	require.NoError(t, err)
	require.NotNil(t, chatWorkflows.Dispatcher)
}

func TestContainerRejectsResolutionAfterShutdown(t *testing.T) {
	t.Parallel()

	container, err := NewContainer(context.Background(), "", ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	require.NoError(t, err)
	assert.True(t, container.ShutdownWithContext(context.Background()).Succeed)
	assert.True(t, container.ShutdownWithContext(context.Background()).Succeed)

	runtimeServices, err := container.StartRuntime()
	assert.Nil(t, runtimeServices)
	require.ErrorContains(t, err, "container is closed")

	services := containerServiceAccessors(container)
	for _, resolve := range services {
		service, resolveErr := resolve()
		assert.Nil(t, service)
		assert.ErrorContains(t, resolveErr, "container is closed")
	}
}

func TestContainerServiceAccessors(t *testing.T) {
	t.Parallel()

	container, err := NewContainer(context.Background(), "", ConfigOverrides{
		DisableExtensions: false,
		Interactive:       false,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.Empty(t, report.Errors)
	})

	services := containerServiceAccessors(container)
	for _, resolve := range services {
		service, resolveErr := resolve()
		require.NoError(t, resolveErr)
		require.NotNil(t, service)
	}
}

func containerServiceAccessors(container *Container) []func() (any, error) {
	return []func() (any, error){
		func() (any, error) { return container.ConfigService() },
		func() (any, error) { return container.AuthService() },
		func() (any, error) { return container.DatabaseService() },
		func() (any, error) { return container.ExtensionService() },
		func() (any, error) { return container.ModelService() },
		func() (any, error) { return container.AssistantService() },
		func() (any, error) { return container.AgentTaskService() },
		func() (any, error) { return container.WorkflowService() },
		func() (any, error) { return container.ChatWorkflowService() },
		func() (any, error) { return container.ToolService() },
		func() (any, error) { return container.SkillsService() },
	}
}
