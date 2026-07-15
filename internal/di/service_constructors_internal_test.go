package di

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
)

func TestNewCacheServiceUsesConfiguredCache(t *testing.T) {
	t.Parallel()

	injector := do.New()
	do.ProvideValue(injector, &ConfigService{cfg: testServiceConfig(), path: ""})

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
	do.ProvideValue(injector, &DatabaseService{
		DB: nil, Sessions: nil, Documents: nil, Tasks: nil, AgentTasks: nil, Workflows: nil, path: "",
	})
	do.ProvideValue(injector, &AssistantService{Runtime: nil, Agents: nil})

	service, err := NewAgentTaskService(injector)
	require.ErrorContains(t, err, "create agent task runner")
	assert.Nil(t, service)
}

func TestNewAgentTaskServiceWrapsSchedulerError(t *testing.T) {
	t.Parallel()

	injector := do.New()
	databaseService := newTestDatabaseService(t)
	databaseService.Tasks = nil
	do.ProvideValue(injector, databaseService)
	do.ProvideValue(injector, newTestAssistantService(t, injector))

	service, err := NewAgentTaskService(injector)
	require.ErrorContains(t, err, "create agent task service")
	assert.Nil(t, service)
}

func TestAgentTaskServiceWiresAndShutsDown(t *testing.T) {
	t.Parallel()

	injector := do.New()
	do.ProvideValue(injector, newTestDatabaseService(t))
	assistant := newTestAssistantService(t, injector)
	do.ProvideValue(injector, assistant)

	service, err := NewAgentTaskService(injector)
	require.NoError(t, err)
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

func TestContainerServiceAccessors(t *testing.T) {
	t.Parallel()

	container, err := NewContainer("", ConfigOverrides{DisableExtensions: false})
	require.NoError(t, err)
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.Empty(t, report.Errors)
	})

	require.NotNil(t, container.ConfigService())
	require.NotNil(t, container.AuthService())
	require.NotNil(t, container.DatabaseService())
	require.NotNil(t, container.ExtensionService())
	require.NotNil(t, container.ModelService())
	require.NotNil(t, container.AssistantService())
	require.NotNil(t, container.AgentTaskService())
	require.NotNil(t, container.ToolService())
	require.NotNil(t, container.SkillsService())
}
