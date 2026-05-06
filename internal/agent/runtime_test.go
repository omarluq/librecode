package agent_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sqlite "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/plugin"
	"github.com/omarluq/librecode/internal/session"
)

func TestRuntime_PromptPersistsConversation(t *testing.T) {
	t.Parallel()

	runtime, store := newTestRuntime(t)

	response, err := runtime.Prompt(context.Background(), agent.PromptRequest{
		SessionID: "",
		CWD:       "/work",
		Text:      "hello",
		Name:      "test",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, response.SessionID)
	assert.NotEmpty(t, response.UserEntryID)
	assert.NotEmpty(t, response.AssistantEntryID)
	assert.Contains(t, response.Text, "librecode-go local runtime")

	entries, err := store.Entries(context.Background(), response.SessionID)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, session.RoleUser, entries[0].Message.Role)
	assert.Equal(t, session.RoleAssistant, entries[1].Message.Role)
}

func TestRuntime_PromptUsesResponseCache(t *testing.T) {
	t.Parallel()

	runtime, _ := newTestRuntime(t)
	request := agent.PromptRequest{SessionID: "", CWD: "/work", Text: "cache me", Name: ""}

	firstResponse, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)

	request.SessionID = firstResponse.SessionID
	secondResponse, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, secondResponse.Cached)
	assert.Equal(t, firstResponse.Text, secondResponse.Text)
}

func newTestRuntime(t *testing.T) (*agent.Runtime, *session.Store) {
	t.Helper()

	database, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, database.Close())
	})
	database.SetMaxOpenConns(1)
	require.NoError(t, session.Migrate(context.Background(), database))

	store := session.NewStore(database)
	manager := plugin.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	cache := agent.NewResponseCache(true, 32, time.Minute)
	t.Cleanup(cache.Shutdown)

	return agent.NewRuntime(testConfig(), store, manager, cache, slog.New(slog.NewTextHandler(io.Discard, nil))), store
}

func testConfig() *config.Config {
	return &config.Config{
		App: config.AppConfig{Name: "librecode", Env: "test"},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "pretty",
		},
		Database: config.DatabaseConfig{
			Path:            "",
			ApplyMigrations: true,
			MaxOpenConns:    1,
			MaxIdleConns:    1,
			ConnMaxLifetime: time.Minute,
		},
		Plugins: config.PluginsConfig{
			Enabled: true,
			Paths:   []string{},
		},
		Agent: config.AgentConfig{
			Provider:      "local",
			Model:         "librecode-go",
			ThinkingLevel: "off",
		},
		Cache: config.CacheConfig{
			Enabled:  true,
			Capacity: 32,
			TTL:      time.Minute,
		},
		KSQL: config.KSQLConfig{
			Endpoint: "",
			Timeout:  time.Second,
		},
	}
}

func sqliteDriver() string {
	var driverError *sqlite.Error
	_ = driverError

	return "sqlite"
}
