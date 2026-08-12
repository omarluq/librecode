package terminal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	"github.com/omarluq/librecode/internal/transcript"
)

const (
	promptSendTestAppName     = "test-app"
	promptSendTestFormat      = "plain"
	promptSendTestModel       = "test-model"
	promptSendTestEnv         = "test-env"
	promptSendTestProvider    = "test-provider"
	promptSendTestText        = "hello"
	promptSendQueuedFollowUp  = "queued follow-up"
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

type terminalBlockingPromptClient struct {
	started chan *assistant.CompletionRequest
	release chan struct{}
}

func (client *terminalBlockingPromptClient) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	select {
	case client.started <- request:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for request: %w", ctx.Err())
	}

	select {
	case <-client.release:
		return newTerminalCompletionResult("done"), nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for release: %w", ctx.Err())
	}
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

func TestDeliverDraftDefaultsUnknownDeliveryToPrompt(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("ok"), nil)
	app := newPromptSendTestApp(t, client)

	consumed, err := app.deliverDraft(
		t.Context(), promptDraft{Text: string(promptDeliveryPrompt), Images: nil}, promptDelivery("unknown"),
	)
	require.NoError(t, err)
	assert.False(t, consumed)
	assert.Equal(t, []string{string(promptDeliveryPrompt)}, app.promptHistory)
	request := waitForPromptRequest(t, client)
	require.NotEmpty(t, request.Messages)
	assert.Equal(t, string(promptDeliveryPrompt), request.Messages[len(request.Messages)-1].Content)
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
			composerText:      promptSendQueuedFollowUp,
			wantComposerText:  "",
			wantQueued:        []string{promptSendQueuedFollowUp},
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
	assertQueuedMessages(t, testCase.wantQueued, promptDraftTexts(app.queuedMessages))
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

func startBlockingPrompt(
	t *testing.T,
) (*App, *terminalBlockingPromptClient, *assistant.CompletionRequest) {
	t.Helper()

	client := &terminalBlockingPromptClient{
		started: make(chan *assistant.CompletionRequest, 1),
		release: make(chan struct{}),
	}
	app := newPromptSendTestApp(t, client)
	app.screen = newClipboardScreen()
	app.sendPrompt(t.Context(), "initial")

	var request *assistant.CompletionRequest
	select {
	case request = <-client.started:
	case <-time.After(time.Minute):
		t.Fatal("blocking prompt request should start")
	}

	entryEvent := readPromptAsyncEvent(t, app)
	require.Equal(t, asyncEventPromptUserEntry, entryEvent.Kind)
	app.applyPromptUserEntry(t.Context(), entryEvent.Provider, entryEvent.Text, entryEvent.PromptID)

	return app, client, request
}

func TestActiveEnterSteersRuntime(t *testing.T) {
	t.Parallel()

	app, client, request := startBlockingPrompt(t)
	app.composerBuffer.SetText("steer this")
	initialTranscriptLength := len(app.transcript.History)

	_, err := app.handleInputKey(t.Context(), tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	require.NoError(t, err)
	assert.True(t, app.composerDraftEmpty())
	assert.Empty(t, app.queuedMessages)
	assert.Contains(t, app.statusMessage, "steering accepted")
	require.Len(t, app.steeringMessages, 1)
	assert.Equal(t, "steer this", app.steeringMessages[0].Text)
	assert.Contains(t, strings.Join(lineTexts(app.messageLines(80, -1)), "\n"), "steer this")
	assert.Len(t, app.transcript.History, initialTranscriptLength,
		"accepted steering must render as pending without adding an optimistic durable transcript entry")

	app.appendStreamingBlock(transcript.RoleAssistant, "checkpointed response")
	pendingLines := strings.Join(lineTexts(app.messageLines(80, -1)), "\n")
	assert.Less(t, strings.Index(pendingLines, "checkpointed response"), strings.Index(pendingLines, "steer this"))

	round := &llm.CompletedRound{
		Assistant: llm.Message{Metadata: nil, Role: "", Content: nil}, ToolResults: nil,
		FinishReason: llm.FinishReasonStop, Usage: llm.EmptyUsage(),
	}
	messages, err := request.OnRoundCheckpoint(t.Context(), round)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 1)
	assert.Equal(t, "steer this", messages[0].Content[0].Text)

	consumedEvent := readPromptAsyncEventUntilKind(t, app, asyncEventSteeringConsumed)
	app.handlePromptAsyncEvent(t.Context(), consumedEvent)
	assert.Empty(t, app.steeringMessages)
	require.Len(t, app.transcript.History, initialTranscriptLength+2)
	checkpointed := app.transcript.History[len(app.transcript.History)-2]
	assert.Equal(t, transcript.RoleAssistant, checkpointed.Role)
	assert.Equal(t, "checkpointed response", checkpointed.Content)
	assert.Empty(t, app.transcript.Streaming.Blocks)
	consumed := app.transcript.History[len(app.transcript.History)-1]
	assert.Equal(t, transcript.RoleUser, consumed.Role)
	assert.Equal(t, "steer this", consumed.Content)
	require.NotNil(t, consumed.EntryID)

	close(client.release)
}

func TestHiddenContinuationSteersRuntime(t *testing.T) {
	t.Parallel()

	app, client, request := startBlockingPrompt(t)
	app.deliverHiddenContinuation(t.Context(), "background result")

	assert.Empty(t, app.hiddenQueuedMessages)
	assert.Contains(t, app.statusMessage, "steered into the active response")

	round := &llm.CompletedRound{
		Assistant: llm.Message{Metadata: nil, Role: "", Content: nil}, ToolResults: nil,
		FinishReason: llm.FinishReasonStop, Usage: llm.EmptyUsage(),
	}
	messages, err := request.OnRoundCheckpoint(t.Context(), round)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 1)
	assert.Equal(t, "background result", messages[0].Content[0].Text)

	entries, err := app.runtime.SessionRepository().Entries(t.Context(), app.sessionID)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.False(t, entries[len(entries)-1].Display)
	assert.True(t, entries[len(entries)-1].ModelFacing)

	app.appendStreamingBlock(transcript.RoleAssistant, "pre-continuation response")
	consumedEvent := readPromptAsyncEventUntilKind(t, app, asyncEventSteeringConsumed)
	app.handlePromptAsyncEvent(t.Context(), consumedEvent)
	require.Len(t, app.transcript.History, 2)
	checkpointed := app.transcript.History[len(app.transcript.History)-1]
	assert.Equal(t, transcript.RoleAssistant, checkpointed.Role)
	assert.Equal(t, "pre-continuation response", checkpointed.Content)
	assert.Empty(t, app.transcript.Streaming.Blocks)
	assert.NotContains(t, transcriptContents(app.transcript.History), "background result")

	close(client.release)
}

func TestActiveInputKeyRouting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event       *tcell.EventKey
		name        string
		wantText    string
		wantQueued  []string
		wantHistory int
	}{
		{
			name: "shift enter queues follow-up", event: tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModShift),
			wantText: "", wantQueued: []string{"draft"}, wantHistory: 1,
		},
		{
			name: "ctrl j inserts newline", event: tcell.NewEventKey(tcell.KeyCtrlJ, "", tcell.ModNone),
			wantText: "draft\n", wantQueued: nil, wantHistory: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			app := newRenderTestApp(t)
			app.working = true
			app.activePrompt = &activePromptState{
				Cancel: nil, SessionID: app.sessionID, UserEntryID: "", Prompt: "", Images: nil,
				UserMessageTimestamp: 0, ID: 1, Canceled: false,
			}
			app.composerBuffer.SetText("draft")

			_, err := app.handleInputKey(t.Context(), testCase.event)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantText, app.composerBuffer.TextValue())
			assertQueuedMessages(t, testCase.wantQueued, promptDraftTexts(app.queuedMessages))
			assert.Len(t, app.promptHistory, testCase.wantHistory)
		})
	}
}

func TestSteerDraftQueuesWhenActivePromptDoesNotMatchDisplayedSession(t *testing.T) {
	t.Parallel()

	const promptEntry = "prompt-entry"

	tests := []struct {
		name            string
		displayed       string
		promptSession   string
		promptUserEntry string
	}{
		{
			name:            "session mismatch",
			displayed:       benchmarkDisplayedSession,
			promptSession:   "prompt-session",
			promptUserEntry: promptEntry,
		},
		{
			name:            "missing prompt session",
			displayed:       benchmarkDisplayedSession,
			promptSession:   "",
			promptUserEntry: promptEntry,
		},
		{
			name:            "missing prompt user entry",
			displayed:       benchmarkDisplayedSession,
			promptSession:   benchmarkDisplayedSession,
			promptUserEntry: "",
		},
		{
			name:            "missing runtime",
			displayed:       benchmarkDisplayedSession,
			promptSession:   benchmarkDisplayedSession,
			promptUserEntry: promptEntry,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.sessionID = test.displayed
			app.activePrompt = newTestActivePrompt(nil)
			app.activePrompt.SessionID = test.promptSession
			app.activePrompt.UserEntryID = test.promptUserEntry

			if test.name == "missing runtime" {
				app.runtime = nil
			}

			require.NoError(t, app.steerDraft(t.Context(), promptDraft{Text: testQueuedPromptText, Images: nil}))

			assert.Equal(t, []string{testQueuedPromptText}, promptDraftTexts(app.queuedMessages))
			assert.Equal(t, "prompt queued next", app.statusMessage)
		})
	}
}

func TestSteerDraftQueuesExactlyOnceWhenRunClosesBeforeAcceptance(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = newTestActivePrompt(nil)
	app.activePrompt.SessionID = app.sessionID
	app.activePrompt.UserEntryID = "closed-run"
	draft := promptDraft{Text: testQueuedPromptText, Images: nil}

	require.NoError(t, app.steerDraft(t.Context(), draft))

	assert.Equal(t, []string{testQueuedPromptText}, promptDraftTexts(app.queuedMessages))
	assert.Equal(t, "prompt queued next", app.statusMessage)
}

func TestRestoreReturnedSteeringPrecedesFollowUps(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = &activePromptState{
		Cancel: nil, SessionID: "", UserEntryID: "", Prompt: "", Images: nil,
		UserMessageTimestamp: 0, ID: 7, Canceled: false,
	}
	app.queuedMessages = promptDrafts("follow-up")

	const encoded = `[{"text":"steer","images":[{"data":"AQ=="}]}]`

	app.restoreReturnedSteering(encoded, 7)

	assert.Equal(t, []string{"steer", "follow-up"}, promptDraftTexts(app.queuedMessages))
	assert.Equal(t, byte(1), app.queuedMessages[0].Images[0].Data[0])
	assert.Contains(t, app.statusMessage, "restored")

	app.restoreReturnedSteering(`[{"text":"stale"}]`, 8)
	assert.Equal(t, []string{"steer", "follow-up"}, promptDraftTexts(app.queuedMessages))
}

func TestSendPromptQueuesWhenWorking(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.working = true

	app.sendPrompt(context.Background(), testQueuedPromptText)

	assert.Equal(t, []string{testQueuedPromptText}, promptDraftTexts(app.queuedMessages))
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

func TestSubmitImageOnlyDraftSendsImagePart(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("assistant response"), nil)
	app := newPromptSendTestApp(t, client)
	imageData := testPNG(t, 2, 3)
	app.composerImages = []imageAttachment{{
		Name: "paste-1.png", MIMEType: clipboardImageMIME, Data: imageData, Width: 2, Height: 3,
	}}

	shouldQuit, err := app.submit(t.Context())
	require.NoError(t, err)
	assert.False(t, shouldQuit)
	assert.True(t, app.composerDraftEmpty())
	require.NotNil(t, app.activePrompt)
	require.Len(t, app.activePrompt.Images, 1)
	assert.Equal(t, imageData, app.activePrompt.Images[0].Data)

	request := waitForPromptRequest(t, client)
	require.NotEmpty(t, request.Messages)
	userMessage := request.Messages[len(request.Messages)-1]
	assert.Empty(t, userMessage.Content)
	require.Len(t, userMessage.Parts, 1)
	assert.Equal(t, database.MessagePartImage, userMessage.Parts[0].Type)
	assert.Equal(t, imageData, userMessage.Parts[0].Data)
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
				OnEvent:          nil,
				OnRetry:          nil,
				OnUserEntry:      nil,
				OnSteeringReturn: nil,
				ParentEntryID:    nil,
				SessionID:        "",
				CWD:              app.cwd,
				Images:           nil,
				Text:             promptSendTestText,
				Name:             "",
				ResumeLatest:     false,
				HideUserPrompt:   false,
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
	sessionRepository := testutil.SessionRepository(t, connection)
	settingsRepository := testutil.DocumentRepository(t, connection)
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
		Input:            []model.InputMode{model.InputText, model.InputImage},
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
		Tasks: config.TaskRuntimeConfig{
			Workers:          0,
			PollInterval:     0,
			LeaseDuration:    0,
			Heartbeat:        0,
			RecoveryInterval: 0,
			DefaultTimeout:   0,
			MaxTimeout:       0,
			MaxOutcomeBytes:  0,
		},
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
	case <-time.After(time.Minute):
		t.Fatal("runtime request should be captured")
	}

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
