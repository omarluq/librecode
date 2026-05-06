package assistant_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
)

func TestRuntime_PromptPersistsConversation(t *testing.T) {
	t.Parallel()

	runtime, store := newTestRuntime(t)

	response, err := runtime.Prompt(context.Background(), assistant.PromptRequest{
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
	assert.Equal(t, database.RoleUser, entries[0].Message.Role)
	assert.Equal(t, database.RoleAssistant, entries[1].Message.Role)
}

func TestRuntime_PromptUsesResponseCache(t *testing.T) {
	t.Parallel()

	runtime, _ := newTestRuntime(t)
	request := assistant.PromptRequest{SessionID: "", CWD: "/work", Text: "cache me", Name: ""}

	firstResponse, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)

	request.SessionID = firstResponse.SessionID
	secondResponse, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, secondResponse.Cached)
	assert.Equal(t, firstResponse.Text, secondResponse.Text)
}

func TestRuntime_PromptRunsBuiltInToolSlashCommand(t *testing.T) {
	t.Parallel()

	runtime, _ := newTestRuntime(t)
	cwd := t.TempDir()

	response, err := runtime.Prompt(context.Background(), assistant.PromptRequest{
		SessionID: "",
		CWD:       cwd,
		Text:      `/tool write {"path":"note.txt","content":"hello"}`,
		Name:      "",
	})
	require.NoError(t, err)
	assert.Contains(t, response.Text, "Successfully wrote")

	//nolint:gosec // Test reads from t.TempDir-controlled path.
	content, err := os.ReadFile(filepath.Join(cwd, "note.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
}

func newTestRuntime(t *testing.T) (*assistant.Runtime, *database.SessionStore) {
	t.Helper()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, connection.Close())
	})
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(context.Background(), connection))

	store := database.NewSessionStore(connection)
	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	cache := assistant.NewResponseCache(true, 32, time.Minute)
	t.Cleanup(cache.Shutdown)

	return assistant.NewRuntime(
		testConfig(),
		store,
		manager,
		cache,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	), store
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
		Extensions: config.ExtensionsConfig{
			Enabled: true,
			Paths:   []string{},
		},
		Assistant: config.AssistantConfig{
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
	return "sqlite"
}
