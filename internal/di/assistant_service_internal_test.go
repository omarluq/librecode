package di

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // register SQLite driver for assistant service wiring tests.

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestNewAssistantServiceWiresRuntimeOptions(t *testing.T) {
	t.Parallel()

	injector := do.New()
	databaseService := newTestDatabaseService(t)
	do.ProvideValue(injector, databaseService)
	modelRegistry := provideTestAssistantDependencies(t, injector)

	service, err := NewAssistantService(injector)

	require.NoError(t, err)
	require.NotNil(t, service.Runtime)
	require.NotNil(t, service.Agents)
	assert.Same(t, databaseService.Sessions, service.Runtime.SessionRepository())
	assert.Same(t, modelRegistry, service.Runtime.ModelRegistry())
	assertRuntimeCapabilitiesUnavailable(t, service)
}

func provideTestAssistantDependencies(t *testing.T, injector do.Injector) *model.Registry {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	do.ProvideValue(injector, &ConfigService{cfg: testServiceConfig(), path: "", interactive: false})
	do.ProvideValue(injector, &ExtensionService{
		Manager: extension.NewManager(logger),
		State:   extension.ManagerState{Configured: nil, Loaded: nil},
	})
	do.ProvideValue(injector, &CacheService{Responses: nil})

	modelRegistry := newTestModelRegistry(t)
	do.ProvideValue(injector, &ModelService{Registry: modelRegistry})
	do.ProvideValue(injector, &SkillsService{Cache: core.NewSkillsCache()})
	do.ProvideValue(injector, &LoggerService{
		SlogLogger:    logger,
		ZerologLogger: newZerologLogger(testServiceConfig(), io.Discard),
	})

	toolService, err := NewToolService(injector)
	require.NoError(t, err)
	do.ProvideValue(injector, toolService)

	databaseService := do.MustInvoke[*DatabaseService](injector)
	if databaseService.Tasks == nil || databaseService.ToolTasks == nil {
		do.ProvideValue(injector, &TaskRuntimeService{Runtime: nil, Manager: nil, Tools: nil})
	} else {
		taskRuntime, err := NewTaskRuntimeService(injector)
		require.NoError(t, err)
		do.ProvideValue(injector, taskRuntime)
	}

	t.Cleanup(func() {
		if skills := do.MustInvoke[*SkillsService](injector); skills.Cache != nil {
			skills.Cache.Close()
		}
	})

	return modelRegistry
}

func newTestDatabaseService(t *testing.T) *DatabaseService {
	t.Helper()

	connection, err := sql.Open(sqliteDriverName, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(context.Background(), connection))

	workflows := testutil.WorkflowRepository(t, connection)
	toolTasks, err := database.NewToolTaskRepository(connection)
	require.NoError(t, err)

	return &DatabaseService{
		DB:         connection,
		Sessions:   testutil.SessionRepository(t, connection),
		Documents:  testutil.DocumentRepository(t, connection),
		Tasks:      workflows.Tasks(),
		AgentTasks: workflows.AgentTasks(),
		Workflows:  workflows,
		ToolTasks:  toolTasks,
		path:       "",
	}
}

func newTestModelRegistry(t *testing.T) *model.Registry {
	t.Helper()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		"test-provider": {
			OAuth:     nil,
			Type:      auth.CredentialTypeAPIKey,
			Key:       "test-key",
			Access:    "",
			Refresh:   "",
			AccountID: "",
			Expires:   0,
			ExpiresAt: 0,
		},
	})

	return model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns: []model.Model{{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           nil,
			Provider:         "test-provider",
			ID:               "test-model",
			Name:             "test-model",
			API:              "",
			BaseURL:          "",
			Input:            []model.InputMode{model.InputText},
			Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    0,
			MaxTokens:        0,
			Reasoning:        false,
		}},
		Discovery: model.DiscoveryOptions{
			Client:       nil,
			CachePath:    "",
			SourceURL:    "",
			CacheTTL:     0,
			FetchTimeout: time.Second,
			Enabled:      false,
		},
	})
}
