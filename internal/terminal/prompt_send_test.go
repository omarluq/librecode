//nolint:testpackage // These tests exercise unexported prompt send helpers.
package terminal

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	// Register the sqlite driver used by prompt-send integration-style tests.
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
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
	_ context.Context,
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

	return client.response, nil
}

//nolint:govet // Test fixture readability matters more than field packing.
type submitCase struct {
	wantQueued        []string
	setupApp          func(*App)
	composerText      string
	wantComposerText  string
	name              string
	wantMode          appMode
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

	if err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
	if got := consumed; got != testCase.wantConsumed {
		t.Fatalf("consumed = %v, want %v", got, testCase.wantConsumed)
	}
	if got := app.mode; got != testCase.wantMode {
		t.Fatalf("mode = %q, want %q", got, testCase.wantMode)
	}
	if got := app.composerBuffer.TextValue(); got != testCase.wantComposerText {
		t.Fatalf("composer text = %q, want %q", got, testCase.wantComposerText)
	}
	if got := len(app.promptHistory); got != testCase.wantPromptHistory {
		t.Fatalf("promptHistory length = %d, want %d", got, testCase.wantPromptHistory)
	}
	if !slices.Equal(app.queuedMessages, testCase.wantQueued) {
		t.Fatalf("queuedMessages = %v, want %v", app.queuedMessages, testCase.wantQueued)
	}
	if got := client.request != nil; got != testCase.wantRequest {
		t.Fatalf("request captured = %v, want %v", got, testCase.wantRequest)
	}
}

func TestSendPromptQueuesWhenWorking(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.working = true

	app.sendPrompt(context.Background(), testQueuedPromptText)

	if got, want := app.queuedMessages, []string{testQueuedPromptText}; !slices.Equal(got, want) {
		t.Fatalf("queuedMessages = %v, want %v", got, want)
	}
	if app.activePrompt != nil {
		t.Fatal("activePrompt should not be set when queuing follow-up")
	}
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

	if got := app.tokenUsage.ContextTokens; got != 25_000 {
		t.Fatalf("tokenUsage.ContextTokens = %d, want 25000", got)
	}
	request := waitForPromptRequest(t, client)
	if got := request.Messages[len(request.Messages)-1].Content; got != promptSendTestText {
		t.Fatalf("last request message = %q, want hello", got)
	}
	if app.pendingParentID != nil {
		t.Fatal("pendingParentID should be cleared")
	}
	if !app.working {
		t.Fatal("app should be marked working after send")
	}
	if app.activePrompt == nil {
		t.Fatal("activePrompt should be initialized")
	}
	if got, want := app.activePrompt.Prompt, promptSendTestText; got != want {
		t.Fatalf("activePrompt.Prompt = %q, want %q", got, want)
	}
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
			wantText: "boom",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newPromptSendTestApp(t, testCase.client)
			app.screen = newClipboardScreen()
			promptCtx, cancel := context.WithCancel(context.Background())
			request := &assistant.PromptRequest{
				OnEvent:       nil,
				OnRetry:       nil,
				OnUserEntry:   nil,
				ParentEntryID: nil,
				SessionID:     "",
				CWD:           app.cwd,
				Text:          promptSendTestText,
				Name:          "",
				ResumeLatest:  false,
			}

			app.runPrompt(context.Background(), promptCtx, cancel, request, 7)

			promptEvent := readPromptAsyncEvent(t, app)
			if got := promptEvent.Kind; got != testCase.wantKind {
				t.Fatalf("promptEvent.Kind = %q, want %q", got, testCase.wantKind)
			}
			if got := promptEvent.Text; got != testCase.wantText {
				t.Fatalf("promptEvent.Text = %q, want %q", got, testCase.wantText)
			}
			if got := promptEvent.PromptID; got != 7 {
				t.Fatalf("promptEvent.PromptID = %d, want 7", got)
			}
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
	runtime := assistant.NewRuntime(&assistant.RuntimeOptions{
		Config:     runtimeConfig,
		Sessions:   sessionRepository,
		Extensions: manager,
		Cache:      cache,
		Events:     event.NewBus(slog.Default()),
		Models:     registry,
		Client:     client,
		Logger:     slog.Default(),
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
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := connection.Close(); closeErr != nil {
			t.Fatalf("close sqlite: %v", closeErr)
		}
	})
	connection.SetMaxOpenConns(1)
	migrateErr := database.Migrate(context.Background(), connection)
	if migrateErr != nil {
		t.Fatalf("migrate sqlite: %v", migrateErr)
	}

	return connection
}

func newPromptSendTestModelRegistry(t *testing.T) *model.Registry {
	t.Helper()

	authStorage, err := auth.NewInMemoryStorage(context.Background(), map[string]auth.Credential{
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
	if err != nil {
		t.Fatalf("create auth storage: %v", err)
	}

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
			KeepRecentTokens:      20_000,
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
		KSQL:       config.KSQLConfig{Endpoint: "", Timeout: time.Second},
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

	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("runtime request should be captured")
	}
	client.lock.Lock()
	defer client.lock.Unlock()

	return client.request
}

func readPromptAsyncEvent(t *testing.T, app *App) *asyncEvent {
	t.Helper()

	select {
	case raw := <-app.screen.EventQ():
		interrupt, ok := raw.(*tcell.EventInterrupt)
		if !ok {
			t.Fatalf("event = %T, want *tcell.EventInterrupt", raw)
		}
		promptEvent, ok := interrupt.Data().(*asyncEvent)
		if !ok {
			t.Fatalf("interrupt data = %T, want *asyncEvent", interrupt.Data())
		}

		return promptEvent
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async event")
		return nil
	}
}
