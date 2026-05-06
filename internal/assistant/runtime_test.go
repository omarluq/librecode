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
	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
)

const (
	testRuntimeProvider = "test-provider"
	testRuntimeModel    = "test-model"
	testRuntimeCWD      = "/work"
)

func TestRuntime_PromptPersistsConversation(t *testing.T) {
	t.Parallel()

	runtime, repository := newTestRuntime(t)

	response, err := runtime.Prompt(context.Background(), &assistant.PromptRequest{
		OnEvent:       nil,
		ParentEntryID: nil,
		SessionID:     "",
		CWD:           testRuntimeCWD,
		Text:          "hello",
		Name:          "test",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, response.SessionID)
	assert.NotEmpty(t, response.UserEntryID)
	assert.NotEmpty(t, response.AssistantEntryID)
	assert.Contains(t, response.Text, "test assistant response")

	entries, err := repository.Entries(context.Background(), response.SessionID)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, database.RoleUser, entries[0].Message.Role)
	assert.Equal(t, database.RoleAssistant, entries[1].Message.Role)
}

func TestRuntime_PromptUsesResponseCache(t *testing.T) {
	t.Parallel()

	runtime, _ := newTestRuntime(t)
	request := &assistant.PromptRequest{
		OnEvent:       nil,
		ParentEntryID: nil,
		SessionID:     "",
		CWD:           testRuntimeCWD,
		Text:          "cache me",
		Name:          "",
	}

	firstResponse, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)

	request.SessionID = firstResponse.SessionID
	secondResponse, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, secondResponse.Cached)
	assert.Equal(t, firstResponse.Text, secondResponse.Text)
}

func TestRuntime_PromptEmitsStreamEvents(t *testing.T) {
	t.Parallel()

	runtime, _ := newTestRuntime(t)
	events := []assistant.StreamEvent{}

	_, err := runtime.Prompt(context.Background(), &assistant.PromptRequest{
		OnEvent: func(event assistant.StreamEvent) {
			events = append(events, event)
		},
		ParentEntryID: nil,
		SessionID:     "",
		CWD:           testRuntimeCWD,
		Text:          "stream me",
		Name:          "",
	})
	require.NoError(t, err)
	require.NotEmpty(t, events)
	assert.Equal(t, assistant.StreamEventTextDelta, events[0].Kind)
	assert.Contains(t, events[0].Text, "stream me")
}

func TestRuntime_PromptRunsBuiltInToolSlashCommand(t *testing.T) {
	t.Parallel()

	runtime, _ := newTestRuntime(t)
	cwd := t.TempDir()

	response, err := runtime.Prompt(context.Background(), &assistant.PromptRequest{
		OnEvent:       nil,
		ParentEntryID: nil,
		SessionID:     "",
		CWD:           cwd,
		Text:          `/tool write {"path":"note.txt","content":"hello"}`,
		Name:          "",
	})
	require.NoError(t, err)
	assert.Contains(t, response.Text, "Successfully wrote")

	//nolint:gosec // Test reads from t.TempDir-controlled path.
	content, err := os.ReadFile(filepath.Join(cwd, "note.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
}

func newTestRuntime(t *testing.T) (*assistant.Runtime, *database.SessionRepository) {
	t.Helper()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, connection.Close())
	})
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(context.Background(), connection))

	repository := database.NewSessionRepository(connection)
	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	cache := assistant.NewResponseCache(true, 32, time.Minute)
	t.Cleanup(cache.Shutdown)

	return assistant.NewRuntime(
		testConfig(),
		repository,
		manager,
		cache,
		nil,
		testRegistry(t),
		testCompletionClient{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	), repository
}

type testCompletionClient struct{}

func (testCompletionClient) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.OnEvent != nil {
		request.OnEvent(assistant.StreamEvent{
			ToolEvent: nil,
			Kind:      assistant.StreamEventTextDelta,
			Text:      "test assistant response for " + request.Messages[len(request.Messages)-1].Content,
		})
	}

	return &assistant.CompletionResult{
		Text:       "test assistant response for " + request.Messages[len(request.Messages)-1].Content,
		Thinking:   nil,
		ToolEvents: nil,
	}, nil
}

func testRegistry(t *testing.T) *model.Registry {
	t.Helper()

	storage, err := auth.NewInMemoryStorage(context.Background(), map[string]auth.Credential{
		testRuntimeProvider: testProviderCredential(),
	})
	require.NoError(t, err)

	return model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			{
				ThinkingLevelMap: nil,
				Headers:          nil,
				Compat:           nil,
				Provider:         testRuntimeProvider,
				ID:               testRuntimeModel,
				Name:             testRuntimeModel,
				API:              "openai-completions",
				BaseURL:          "https://example.invalid/v1",
				Input:            []model.InputMode{model.InputText},
				Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
				ContextWindow:    0,
				MaxTokens:        0,
				Reasoning:        false,
			},
		},
	})
}

func testProviderCredential() auth.Credential {
	return auth.Credential{
		OAuth:     nil,
		Type:      auth.CredentialTypeAPIKey,
		Key:       "test-key",
		Access:    "",
		Refresh:   "",
		AccountID: "",
		Expires:   0,
		ExpiresAt: 0,
	}
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
			Provider:      testRuntimeProvider,
			Model:         testRuntimeModel,
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
