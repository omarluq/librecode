package assistant_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	testSkillDelimiter  = "---"
)

func newRuntimePromptRequest(cwd, text, name string) *assistant.PromptRequest {
	return &assistant.PromptRequest{
		OnEvent:       nil,
		OnRetry:       nil,
		OnUserEntry:   nil,
		ParentEntryID: nil,
		SessionID:     "",
		CWD:           cwd,
		Text:          text,
		Name:          name,
		ResumeLatest:  false,
	}
}

func TestRuntime_PromptPersistsConversation(t *testing.T) {
	t.Parallel()

	runtime, repository := newTestRuntime(t)

	response, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "hello", "test"))
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

func TestRuntime_PromptNotifiesUserEntry(t *testing.T) {
	t.Parallel()

	runtime, _ := newTestRuntime(t)
	userEntryEvent := assistant.PromptUserEntryEvent{SessionID: "", EntryID: ""}

	request := newRuntimePromptRequest(testRuntimeCWD, "notify me", "")
	request.OnUserEntry = func(event assistant.PromptUserEntryEvent) {
		userEntryEvent = event
	}
	response, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)
	assert.Equal(t, response.SessionID, userEntryEvent.SessionID)
	assert.Equal(t, response.UserEntryID, userEntryEvent.EntryID)
}

func TestRuntime_PromptStartsNewSessionByDefault(t *testing.T) {
	t.Parallel()

	runtime, repository := newTestRuntime(t)
	ctx := context.Background()

	firstResponse, err := runtime.Prompt(ctx, newRuntimePromptRequest(testRuntimeCWD, "first session", ""))
	require.NoError(t, err)
	secondResponse, err := runtime.Prompt(ctx, newRuntimePromptRequest(testRuntimeCWD, "second session", ""))
	require.NoError(t, err)

	assert.NotEqual(t, firstResponse.SessionID, secondResponse.SessionID)
	sessions, err := repository.ListSessions(ctx, testRuntimeCWD)
	require.NoError(t, err)
	assert.Len(t, sessions, 2)
}

func TestRuntime_PromptResumesLatestSessionWhenRequested(t *testing.T) {
	t.Parallel()

	runtime, repository := newTestRuntime(t)
	ctx := context.Background()

	firstResponse, err := runtime.Prompt(ctx, newRuntimePromptRequest(testRuntimeCWD, "first session", ""))
	require.NoError(t, err)
	resumeRequest := newRuntimePromptRequest(testRuntimeCWD, "resume session", "")
	resumeRequest.ResumeLatest = true
	secondResponse, err := runtime.Prompt(ctx, resumeRequest)
	require.NoError(t, err)

	assert.Equal(t, firstResponse.SessionID, secondResponse.SessionID)
	entries, err := repository.Entries(ctx, firstResponse.SessionID)
	require.NoError(t, err)
	assert.Len(t, entries, 4)
}

func TestRuntime_PromptUsesResponseCache(t *testing.T) {
	t.Parallel()

	runtime, _ := newTestRuntime(t)
	request := newRuntimePromptRequest(testRuntimeCWD, "cache me", "")

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

	request := newRuntimePromptRequest(testRuntimeCWD, "stream me", "")
	request.OnEvent = func(event assistant.StreamEvent) {
		events = append(events, event)
	}
	_, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)
	textEvent := firstStreamEventKind(events, assistant.StreamEventTextDelta)
	require.NotNil(t, textEvent)
	assert.Contains(t, textEvent.Text, "stream me")
}

func TestRuntime_PromptRunsBuiltInToolSlashCommand(t *testing.T) {
	t.Parallel()

	runtime, _ := newTestRuntime(t)
	cwd := t.TempDir()

	response, err := runtime.Prompt(
		context.Background(),
		newRuntimePromptRequest(cwd, `/tool write {"path":"note.txt","content":"hello"}`, ""),
	)
	require.NoError(t, err)
	assert.Contains(t, response.Text, "Successfully wrote")

	//nolint:gosec // Test reads from t.TempDir-controlled path.
	content, err := os.ReadFile(filepath.Join(cwd, "note.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
}

func TestRuntime_PromptRetriesTransientModelErrors(t *testing.T) {
	t.Parallel()

	client := &retryCompletionClient{
		err:               nil,
		response:          "recovered response",
		attempts:          0,
		failuresRemaining: 1,
	}
	runtime, _ := newTestRuntimeWithClient(t, client)
	request := newRuntimePromptRequest(testRuntimeCWD, "retry me", "")
	retryEvents := []assistant.RetryEvent{}
	request.OnRetry = func(event assistant.RetryEvent) {
		retryEvents = append(retryEvents, event)
	}

	response, err := runtime.Prompt(context.Background(), request)

	require.NoError(t, err)
	assert.Equal(t, "recovered response for retry me", response.Text)
	assert.Equal(t, 2, client.attempts)
	require.Len(t, retryEvents, 2)
	assert.Equal(t, assistant.RetryEventStart, retryEvents[0].Kind)
	assert.Equal(t, 2, retryEvents[0].Attempt)
	assert.Equal(t, assistant.RetryEventEnd, retryEvents[1].Kind)
}

func TestRuntime_PromptRetriesWrappedEmptyProviderResponse(t *testing.T) {
	t.Parallel()

	client := &retryCompletionClient{
		err:               errors.New("[system] provider returned an empty response"),
		response:          "recovered response",
		attempts:          0,
		failuresRemaining: 1,
	}
	runtime, _ := newTestRuntimeWithClient(t, client)
	request := newRuntimePromptRequest(testRuntimeCWD, "retry empty", "")

	response, err := runtime.Prompt(context.Background(), request)

	require.NoError(t, err)
	assert.Equal(t, "recovered response for retry empty", response.Text)
	assert.Equal(t, 2, client.attempts)
}

func TestRuntime_PromptDoesNotRetryNonTransientModelErrors(t *testing.T) {
	t.Parallel()

	client := &retryCompletionClient{
		err:               errors.New("maximum context length exceeded"),
		response:          "should not be used",
		attempts:          0,
		failuresRemaining: 1,
	}
	runtime, _ := newTestRuntimeWithClient(t, client)
	request := newRuntimePromptRequest(testRuntimeCWD, "too much context", "")

	_, err := runtime.Prompt(context.Background(), request)

	require.Error(t, err)
	assert.Equal(t, 1, client.attempts)
}

func TestRuntime_SlashSkillShowsContent(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	skillPath := filepath.Join(cwd, ".librecode", "skills", "fix-bug", "SKILL.md")
	writeRuntimeTestFile(t, skillPath, strings.Join([]string{
		testSkillDelimiter,
		"name: fix-bug",
		"description: Fix bugs safely",
		testSkillDelimiter,
		"Use tests first.",
	}, "\n"))
	runtime, _ := newTestRuntime(t)
	events := []assistant.StreamEvent{}
	request := newRuntimePromptRequest(cwd, "/skill:fix-bug", "")
	request.OnEvent = func(event assistant.StreamEvent) {
		events = append(events, event)
	}

	response, err := runtime.Prompt(context.Background(), request)

	require.NoError(t, err)
	assert.Contains(t, response.Text, "Use tests first.")
	require.Len(t, response.ToolEvents, 1)
	assert.Equal(t, "load skill: fix-bug", response.ToolEvents[0].Name)
	assert.Contains(t, response.ToolEvents[0].ArgumentsJSON, skillPath)
	require.Len(t, events, 1)
	assert.Equal(t, assistant.StreamEventSkillLoaded, events[0].Kind)
}

func TestRuntime_PromptIncludesDiscoveredSkills(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	writeRuntimeTestFile(t, filepath.Join(cwd, ".librecode", "skills", "fix-bug", "SKILL.md"), strings.Join([]string{
		testSkillDelimiter,
		"name: fix-bug",
		"description: Fix bugs safely",
		testSkillDelimiter,
		"Use tests first.",
	}, "\n"))
	client := &capturingCompletionClient{request: nil}
	runtime, _ := newTestRuntimeWithClient(t, client)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(cwd, "please fix-bug", ""))
	require.NoError(t, err)
	require.NotNil(t, client.request)
	assert.Contains(t, client.request.SystemPrompt, "<available_skills>")
	assert.Contains(t, client.request.SystemPrompt, "<name>fix-bug</name>")
	assert.Contains(t, client.request.SystemPrompt, filepath.Join(cwd, ".librecode", "skills", "fix-bug", "SKILL.md"))
	assert.Contains(t, client.request.SystemPrompt, "<active_skills>")
	assert.Contains(t, client.request.SystemPrompt, "Use tests first.")
}

func firstStreamEventKind(events []assistant.StreamEvent, kind assistant.StreamEventKind) *assistant.StreamEvent {
	for index := range events {
		if events[index].Kind == kind {
			return &events[index]
		}
	}

	return nil
}

func newTestRuntime(t *testing.T) (*assistant.Runtime, *database.SessionRepository) {
	t.Helper()

	return newTestRuntimeWithClient(t, testCompletionClient{})
}

func newTestRuntimeWithClient(
	t *testing.T,
	client assistant.CompletionClient,
) (*assistant.Runtime, *database.SessionRepository) {
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
		client,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	), repository
}

type capturingCompletionClient struct {
	request *assistant.CompletionRequest
}

type retryCompletionClient struct {
	err               error
	response          string
	attempts          int
	failuresRemaining int
}

func (client *capturingCompletionClient) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.request = request

	return testCompletionClient{}.Complete(ctx, request)
}

func (client *retryCompletionClient) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.attempts++
	if client.failuresRemaining > 0 {
		client.failuresRemaining--
		if client.err != nil {
			return nil, client.err
		}

		return nil, errors.New("provider is temporarily unavailable")
	}

	return &assistant.CompletionResult{
		Text:       client.response + " for " + request.Messages[len(request.Messages)-1].Content,
		Thinking:   nil,
		ToolEvents: nil,
		Usage:      model.EmptyTokenUsage(),
	}, nil
}

type testCompletionClient struct{}

func (testCompletionClient) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.OnEvent != nil {
		request.OnEvent(assistant.StreamEvent{
			ToolEvent: nil,
			Usage:     nil,
			Kind:      assistant.StreamEventTextDelta,
			Text:      "test assistant response for " + request.Messages[len(request.Messages)-1].Content,
		})
	}

	return &assistant.CompletionResult{
		Text:       "test assistant response for " + request.Messages[len(request.Messages)-1].Content,
		Thinking:   nil,
		ToolEvents: nil,
		Usage:      model.TokenUsage{InputTokens: 12, OutputTokens: 4, ContextTokens: 12, ContextWindow: 1000},
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
		App: config.AppConfig{
			Name: "librecode",
			Env:  "test",
			WorkingLoader: config.LoaderUI{
				Text: "Shenaniganing...",
			},
		},
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
			Retry: config.RetryConfig{
				BaseDelay:   time.Millisecond,
				MaxDelay:    time.Millisecond,
				MaxAttempts: 3,
				Enabled:     true,
			},
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

func writeRuntimeTestFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
