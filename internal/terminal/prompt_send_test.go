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
	"github.com/omarluq/librecode/internal/model"
)

const (
	promptSendTestAppName  = "test-app"
	promptSendTestFormat   = "plain"
	promptSendTestModel    = "test-model"
	promptSendTestEnv      = "test-env"
	promptSendTestProvider = "test-provider"
	promptSendTestText     = "hello"
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

func TestSubmitIgnoresEmptyPrompt(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("ok"), nil)
	app := newPromptSendTestApp(t, client)
	app.setComposerText("   ")

	consumed, err := app.submit(context.Background())
	if err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
	if consumed {
		t.Fatal("submit should not consume terminal exit")
	}
	if app.working {
		t.Fatal("app should not be working for empty prompt")
	}
	if client.request != nil {
		t.Fatal("runtime should not receive empty prompt")
	}
}

func TestSubmitSlashCommandOpensPanel(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("ok"), nil)
	app := newPromptSendTestApp(t, client)
	app.setComposerText("/model")

	consumed, err := app.submit(context.Background())
	if err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
	if consumed {
		t.Fatal("submit should not consume terminal exit")
	}
	if app.mode != modePanel {
		t.Fatal("slash command should open panel")
	}
	if client.request != nil {
		t.Fatal("runtime should not receive panel command")
	}
}

func TestSubmitQueuesWhenWorking(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("ok"), nil)
	app := newPromptSendTestApp(t, client)
	app.working = true
	app.setComposerText("queued follow-up")

	consumed, err := app.submit(context.Background())
	if err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
	if consumed {
		t.Fatal("submit should not consume terminal exit")
	}
	if got, want := app.queuedMessages, []string{"queued follow-up"}; !slices.Equal(got, want) {
		t.Fatalf("queuedMessages = %v, want %v", got, want)
	}
	if client.request != nil {
		t.Fatal("runtime should not receive queued prompt immediately")
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

	app.screen = newClipboardScreen()
	app.sendPrompt(context.Background(), promptSendTestText)
	_ = readPromptAsyncEvent(t, app)

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
			name:     "done",
			client:   newTerminalPromptClient(newTerminalCompletionResult("done"), nil),
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

func newPromptSendTestApp(t *testing.T, client assistant.CompletionClient) *App {
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

	manager := extension.NewManager(slog.Default())
	t.Cleanup(manager.Shutdown)
	cache := assistant.NewResponseCache(false, 1, time.Minute)
	t.Cleanup(cache.Shutdown)
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
	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         authStorage,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			{
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
			},
		},
	})
	sessionRepository := database.NewSessionRepository(connection)
	settingsRepository := database.NewDocumentRepository(connection)
	runtime := assistant.NewRuntime(
		promptSendTestConfig(),
		sessionRepository,
		manager,
		cache,
		event.NewBus(slog.Default()),
		registry,
		client,
		slog.Default(),
	)
	app := newRenderTestApp(t)
	app.runtime = runtime
	app.settings = settingsRepository
	app.cwd = t.TempDir()
	app.cfg = promptSendTestConfig()

	return app
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
		},
		Cache: config.CacheConfig{Enabled: false, Capacity: 1, TTL: time.Minute},
	}
}

func newTerminalCompletionResult(text string) *assistant.CompletionResult {
	return &assistant.CompletionResult{
		Text:       text,
		Thinking:   nil,
		ToolEvents: nil,
		Usage:      model.EmptyTokenUsage(),
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
