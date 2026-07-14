package terminal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	// Register the sqlite driver used by prompt-send integration-style tests.
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/testutil"
)

const (
	promptSendTestAppName     = "test-app"
	promptSendTestFormat      = "plain"
	promptSendTestModel       = "test-model"
	promptSendTestEnv         = "test-env"
	promptSendTestProvider    = "test-provider"
	promptSendTestText        = "hello"
	promptSendSlashModel      = "/model"
	promptSendWhitespaceInput = "   "
)

type terminalPromptClient struct {
	response *assistant.CompletionResult
	request  *assistant.CompletionRequest
	err      error
	ready    chan struct{}
	lock     sync.Mutex
}

func newTerminalPromptClient(response *assistant.CompletionResult, err error) *terminalPromptClient {
	client := new(terminalPromptClient)
	client.response = response
	client.err = err
	client.ready = make(chan struct{})

	return client
}

func (client *terminalPromptClient) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.lock.Lock()

	client.request = request
	select {
	case <-client.ready:
	default:
		close(client.ready)
	}
	client.lock.Unlock()

	if client.err != nil {
		return nil, client.err
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("completion canceled: %w", ctx.Err())
	default:
	}

	return client.response, nil
}

type submitCase struct {
	setupApp          func(*App)
	composerText      string
	wantComposerText  string
	name              string
	wantMode          appMode
	wantQueued        []string
	wantPromptHistory int
	wantConsumed      bool
	wantRequest       bool
}

func TestSubmit(t *testing.T) {
	t.Parallel()

	for _, testCase := range submitCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := newTerminalPromptClient(newTerminalCompletionResult("ok"), nil)

			app := newPromptSendTestApp(t, client)
			if testCase.setupApp != nil {
				testCase.setupApp(app)
			}

			app.composerBuffer.SetText(testCase.composerText)

			consumed, err := app.submit(context.Background())

			assertSubmitCase(t, app, client, &testCase, consumed, err)
		})
	}
}

func submitCases() []submitCase {
	return []submitCase{
		{
			setupApp:          nil,
			composerText:      promptSendWhitespaceInput,
			wantComposerText:  promptSendWhitespaceInput,
			wantQueued:        nil,
			name:              "ignores empty prompt",
			wantMode:          modeChat,
			wantPromptHistory: 0,
			wantConsumed:      false,
			wantRequest:       false,
		},
		{
			setupApp:          nil,
			composerText:      promptSendSlashModel,
			wantComposerText:  "",
			wantQueued:        nil,
			name:              "slash command opens panel",
			wantMode:          modePanel,
			wantPromptHistory: 1,
			wantConsumed:      false,
			wantRequest:       false,
		},
		{
			setupApp: func(app *App) {
				app.working = true
			},
			composerText:      "queued follow-up",
			wantComposerText:  "",
			wantQueued:        []string{"queued follow-up"},
			name:              "queues when working",
			wantMode:          modeChat,
			wantPromptHistory: 1,
			wantConsumed:      false,
			wantRequest:       false,
		},
		{
			setupApp:          func(app *App) { app.compacting = true },
			composerText:      "wait for compaction",
			wantComposerText:  "",
			wantQueued:        []string{"wait for compaction"},
			name:              "queues prompt while compacting",
			wantMode:          modeChat,
			wantPromptHistory: 1,
			wantConsumed:      false,
			wantRequest:       false,
		},
		{
			setupApp:          func(app *App) { app.compacting = true },
			composerText:      promptSendSlashModel,
			wantComposerText:  promptSendSlashModel,
			wantQueued:        nil,
			name:              "defers command while compacting",
			wantMode:          modeChat,
			wantPromptHistory: 0,
			wantConsumed:      false,
			wantRequest:       false,
		},
	}
}

func assertSubmitCase(
	t *testing.T,
	app *App,
	client *terminalPromptClient,
	testCase *submitCase,
	consumed bool,
	err error,
) {
	t.Helper()

	require.NoError(t, err)
	assert.Equal(t, testCase.wantConsumed, consumed)
	assert.Equal(t, testCase.wantMode, app.mode)
	assert.Equal(t, testCase.wantComposerText, app.composerBuffer.TextValue())
	assert.Len(t, app.promptHistory, testCase.wantPromptHistory)
	assertQueuedMessages(t, testCase.wantQueued, app.queuedMessages)
	assert.Equal(t, testCase.wantRequest, client.request != nil)
}

func assertQueuedMessages(t *testing.T, expected, actual []string) {
	t.Helper()

	if len(expected) == 0 {
		assert.Empty(t, actual)

		return
	}

	assert.Equal(t, expected, actual)
}

func TestSendPromptQueuesWhenWorking(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.working = true

	app.sendPrompt(context.Background(), testQueuedPromptText)

	assert.Equal(t, []string{testQueuedPromptText}, app.queuedMessages)
	assert.Nil(t, app.activePrompt)
}

func TestSendPromptInitializesPromptState(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("assistant response"), nil)
	app := newPromptSendTestApp(t, client)
	parentID := ""
	app.pendingParentID = &parentID
	app.tokenUsage = model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100_000,
		ContextTokens:   25_000,
		InputTokens:     25_000,
		OutputTokens:    0,
	}

	app.screen = newClipboardScreen()
	app.sendPrompt(context.Background(), promptSendTestText)
	_ = readPromptAsyncEvent(t, app)

	assert.Equal(t, 25_000, app.tokenUsage.ContextTokens)

	request := waitForPromptRequest(t, client)
	assert.Equal(t, promptSendTestText, request.Messages[len(request.Messages)-1].Content)
	assert.Nil(t, app.pendingParentID)
	assert.True(t, app.working)
	require.NotNil(t, app.activePrompt)
	assert.Equal(t, promptSendTestText, app.activePrompt.Prompt)
}

func TestRunPromptPostsDoneAndError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		client   *terminalPromptClient
		wantKind asyncEventKind
		wantText string
	}{
		{
			name:     statusDone,
			client:   newTerminalPromptClient(newTerminalCompletionResult(statusDone), nil),
			wantKind: asyncEventPromptDone,
			wantText: "",
		},
		{
			name:     "error",
			client:   newTerminalPromptClient(nil, errors.New("boom")),
			wantKind: asyncEventPromptError,
			wantText: "complete model request: boom",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newPromptSendTestApp(t, testCase.client)
			app.screen = newClipboardScreen()
			promptCtx, cancel := context.WithCancel(context.Background())
			request := &assistant.PromptRequest{
				OnEvent:        nil,
				OnRetry:        nil,
				OnUserEntry:    nil,
				ParentEntryID:  nil,
				SessionID:      "",
				CWD:            app.cwd,
				Text:           promptSendTestText,
				Name:           "",
				ResumeLatest:   false,
				HideUserPrompt: false,
			}

			app.runPrompt(context.Background(), promptCtx, cancel, request, 7)

			promptEvent := readPromptAsyncEvent(t, app)
			assert.Equal(t, testCase.wantKind, promptEvent.Kind)
			assert.Equal(t, testCase.wantText, promptEvent.Text)
			assert.Equal(t, uint64(7), promptEvent.PromptID)
		})
	}
}

func newPromptSendTestApp(t *testing.T, client assistant.Completer) *App {
	t.Helper()

	return newPromptSendTestAppWithConfig(t, client, promptSendTestConfig())
}

func newPromptSendTestAppWithConfig(
	t *testing.T,
	client assistant.Completer,
	runtimeConfig *config.Config,
) *App {
	t.Helper()

	connection := newPromptSendTestConnection(t)
	manager := extension.NewManager(slog.Default())
	t.Cleanup(manager.Shutdown)

	cache := assistant.NewResponseCache(false, 1, time.Minute)
	t.Cleanup(cache.Shutdown)
	registry := newPromptSendTestModelRegistry(t)
	sessionRepository := database.NewSessionRepository(connection)
	settingsRepository := database.NewDocumentRepository(connection)
	runtime := assistant.NewRuntimeForTest(func(opts *assistant.RuntimeTestOptions) {
		opts.Config = runtimeConfig
		opts.Sessions = sessionRepository
		opts.Extensions = manager
		opts.Cache = cache
		opts.Models = registry
		opts.Client = client
		opts.Logger = slog.Default()
	})
	app := newRenderTestApp(t)
	app.runtime = runtime
	app.settings = settingsRepository
	app.cwd = t.TempDir()
	app.cfg = runtimeConfig

	return app
}

func newPromptSendTestConnection(t *testing.T) *sql.DB {
	t.Helper()

	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, connection.Close())
	})
	connection.SetMaxOpenConns(1)

	require.NoError(t, database.Migrate(context.Background(), connection))

	return connection
}

func newPromptSendTestModelRegistry(t *testing.T) *model.Registry {
	t.Helper()

	authStorage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		promptSendTestProvider: {
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
		Auth:         authStorage,
		ModelsPath:   "",
		BuiltIns:     []model.Model{promptSendTestModelDefinition()},
		Discovery:    disabledModelDiscovery(),
	})
}

func promptSendTestModelDefinition() model.Model {
	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         promptSendTestProvider,
		ID:               promptSendTestModel,
		Name:             promptSendTestModel,
		API:              "openai-completions",
		BaseURL:          "https://example.invalid/v1",
		Input:            []model.InputMode{model.InputText},
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    1000,
		MaxTokens:        0,
		Reasoning:        false,
	}
}

func promptSendTestConfig() *config.Config {
	return &config.Config{
		Assistant: config.AssistantConfig{
			Provider:      promptSendTestProvider,
			Model:         promptSendTestModel,
			ThinkingLevel: "off",
			Retry: config.RetryConfig{
				BaseDelay:   time.Millisecond,
				MaxDelay:    time.Millisecond,
				MaxAttempts: 1,
				Enabled:     true,
			},
		},
		Context: config.ContextConfig{
			OutputReserveTokens:   0,
			ProviderReserveTokens: 0,
			SafetyMarginTokens:    0,
			PreflightEnabled:      false,
		},
		Models: config.ModelsConfig{
			Discovery: config.ModelDiscoveryConfig{
				CacheTTL:     0,
				FetchTimeout: 0,
				SourceURL:    "https://models.dev/api.json",
				Enabled:      false,
			},
		},
		App: config.AppConfig{
			Name:          promptSendTestAppName,
			Env:           promptSendTestEnv,
			WorkingLoader: config.LoaderUI{Text: ""},
		},
		Logging:    config.LoggingConfig{Level: "disabled", Format: promptSendTestFormat},
		Extensions: config.ExtensionsConfig{Use: nil, Enabled: false},
		Database: config.DatabaseConfig{
			Path:            "",
			ApplyMigrations: true,
			MaxOpenConns:    1,
			MaxIdleConns:    1,
			ConnMaxLifetime: time.Minute,
			BusyTimeout:     15 * time.Second,
		},
		Cache: config.CacheConfig{Enabled: false, Capacity: 1, TTL: time.Minute},
	}
}

func newTerminalCompletionResult(text string) *assistant.CompletionResult {
	return &assistant.CompletionResult{
		FinishReason: llm.FinishReasonStop,
		Text:         text,
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        model.EmptyTokenUsage(),
	}
}

func waitForPromptRequest(t *testing.T, client *terminalPromptClient) *assistant.CompletionRequest {
	t.Helper()

	require.Eventually(t, func() bool {
		select {
		case <-client.ready:
			return true
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond, "runtime request should be captured")

	client.lock.Lock()
	defer client.lock.Unlock()

	require.NotNil(t, client.request)

	return client.request
}

func readPromptAsyncEvent(t *testing.T, app *App) *asyncEvent {
	t.Helper()

	var raw tcell.Event

	require.Eventually(t, func() bool {
		select {
		case raw = <-app.screen.EventQ():
			return true
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond, "timed out waiting for async event")

	interrupt, matched := raw.(*tcell.EventInterrupt)
	require.Truef(t, matched, "event = %T, want *tcell.EventInterrupt", raw)

	promptEvent, matched := interrupt.Data().(*asyncEvent)
	require.Truef(t, matched, "interrupt data = %T, want *asyncEvent", interrupt.Data())

	return promptEvent
}
