// Package terminal implements a librecode-style interactive terminal UI.
package terminal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/transcript"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	defaultEditorRows          = 6
	workFrameInterval          = 120 * time.Millisecond
	loaderShimmerSweepDuration = 450 * time.Millisecond
	streamingFrameInterval     = 8 * time.Millisecond
	interruptEscapePresses     = 2
	doubleEscapeDelay          = 500 * time.Millisecond
	doubleControlCDelay        = 2 * time.Second
)

type appMode string

const (
	modeChat  appMode = "chat"
	modePanel appMode = "panel"
)

type chatMessage struct {
	CreatedAt time.Time
	Role      transcript.Role
	Content   string
}

type activePromptState struct {
	Cancel        context.CancelFunc
	ParentEntryID *string
	SessionID     string
	UserEntryID   string
	Prompt        string
	ID            uint64
	Canceled      bool
}

type resizeCoalescedEvent struct {
	Resize *tcell.EventResize
	Event  tcell.Event
}

type activeCompactionState struct {
	Cancel      context.CancelFunc
	ID          uint64
	QueuedStart int
}

type messageLineCacheState struct {
	ThemeName     string
	Width         int
	HideThinking  bool
	ToolsExpanded bool
}

type cachedRenderedMessage struct {
	Lines []tui.Line
	Valid bool
}

type transcriptStreamingState struct {
	Blocks     []chatMessage
	LineCache  []cachedRenderedMessage
	CacheState messageLineCacheState
}

type transcriptState struct {
	History     []chatMessage
	Streaming   transcriptStreamingState
	LineCache   messageLineCache
	LastMaxRows int
}

type runningToolBlock struct {
	StartedAt time.Time
	Call      assistant.ToolCallEvent
}

// RunOptions configures the terminal app.
type RunOptions struct {
	Extensions extension.TerminalEventRunner `json:"-"`
	Resources  *core.ResourceSnapshot        `json:"resources"`
	Runtime    *assistant.Runtime            `json:"-"`
	Settings   *database.DocumentRepository  `json:"-"`
	Models     *model.Registry               `json:"-"`
	Auth       *auth.Storage                 `json:"-"`
	Config     *config.Config                `json:"-"`
	CWD        string                        `json:"cwd"`
	SessionID  string                        `json:"session_id"`
}

// App is the terminal chat UI.
type App struct {
	lastEscape            time.Time
	lastControlC          time.Time
	workStartedAt         time.Time
	screen                terminalScreen
	extensions            extension.TerminalEventRunner
	renderer              *tui.Renderer
	frame                 *tui.CellBuffer
	lastResize            *tcell.EventResize
	systemClipboard       systemClipboardWriter
	runtime               *assistant.Runtime
	settings              *database.DocumentRepository
	models                *model.Registry
	auth                  *auth.Storage
	cfg                   *config.Config
	keys                  *keybindings
	panel                 *panel.Model
	pendingParentID       *string
	activePrompt          *activePromptState
	activeCompaction      *activeCompactionState
	scopedEnabled         map[string]bool
	extensionUI           extui.State
	theme                 terminalTheme
	selectedPanelKind     panel.Kind
	sessionID             string
	agentTaskSessionStack []string
	statusMessage         string
	streamingText         string
	streamingThinkingText string
	cwd                   string
	promptHistoryDraft    string
	mode                  appMode
	resources             core.ResourceSnapshot
	transcript            transcriptState
	runningToolBlocks     []runningToolBlock
	liveAgentCompletions  []chatMessage
	agentTasks            []database.AgentTaskEntity
	agentTasksRefreshedAt time.Time
	agentTaskWatches      map[string]context.CancelFunc
	deliveredAgentTasks   map[string]struct{}
	queuedMessages        []string
	hiddenQueuedMessages  []string
	promptHistory         []string
	scopedOrder           []string
	composerBuffer        tui.TextArea
	tokenUsage            model.TokenUsage
	selection             mouseSelection
	promptSequence        uint64
	workFrame             int
	streamedToolEvents    int
	escapePresses         int
	promptHistoryIndex    int
	scrollOffset          int
	autocompleteSelection int
	autocompleteClosed    bool
	sessionNamedOnly      bool
	hideThinking          bool
	working               bool
	compacting            bool
	toolsExpanded         bool
	authWorking           bool
	sessionShowPath       bool
	sessionSortRecent     bool
}

// Run starts an interactive tcell chat loop.
func Run(ctx context.Context, options *RunOptions) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("tui: create screen: %w", err)
	}

	if err := screen.Init(); err != nil {
		return fmt.Errorf("tui: init screen: %w", err)
	}

	screen.EnableMouse(tcell.MouseDragEvents)
	defer screen.Fini()

	app := newApp(screen, options)
	if err := app.loadInitialMessages(ctx); err != nil {
		app.addSystemMessage(err.Error())
	}

	if err := app.loadSessionSettings(ctx); err != nil {
		app.addSystemMessage(err.Error())
	}

	if err := app.loadLatestSessionSettings(ctx); err != nil {
		app.addSystemMessage(err.Error())
	}

	if err := app.runStartupExtensions(ctx); err != nil {
		app.addSystemMessage(err.Error())
	}

	app.discoverActiveAgentTasks(ctx)
	app.loop(ctx)

	return nil
}

type terminalScreen interface {
	tui.ContentSetter
	EventQ() chan tcell.Event
	HideCursor()
	Show()
	SetClipboard(data []byte)
	ShowCursor(x, y int)
	Size() (width, height int)
}

func newApp(screen terminalScreen, options *RunOptions) *App {
	appTheme := initialAppTheme(options)
	resources := initialResourceSnapshot(options)
	app := &App{
		screen:                screen,
		renderer:              tui.NewRenderer(screen),
		frame:                 nil,
		lastResize:            nil,
		systemClipboard:       newDesktopClipboard(),
		runtime:               options.Runtime,
		extensions:            options.Extensions,
		settings:              options.Settings,
		models:                options.Models,
		auth:                  options.Auth,
		cfg:                   options.Config,
		keys:                  newDefaultKeybindings(),
		theme:                 appTheme,
		resources:             resources,
		mode:                  modeChat,
		panel:                 nil,
		cwd:                   options.CWD,
		sessionID:             options.SessionID,
		agentTaskSessionStack: []string{},
		pendingParentID:       nil,
		activePrompt:          nil,
		activeCompaction:      nil,
		transcript: transcriptState{
			History: []chatMessage{},
			Streaming: transcriptStreamingState{
				Blocks:     []chatMessage{},
				LineCache:  nil,
				CacheState: emptyMessageLineCacheState(),
			},
			LineCache:   emptyMessageLineCache(),
			LastMaxRows: 0,
		},
		runningToolBlocks:     []runningToolBlock{},
		liveAgentCompletions:  []chatMessage{},
		agentTasks:            []database.AgentTaskEntity{},
		agentTasksRefreshedAt: time.Time{},
		agentTaskWatches:      map[string]context.CancelFunc{},
		deliveredAgentTasks:   map[string]struct{}{},
		queuedMessages:        []string{},
		hiddenQueuedMessages:  []string{},
		promptHistory:         []string{},
		promptHistoryDraft:    "",
		autocompleteSelection: 0,
		autocompleteClosed:    false,
		composerBuffer:        tui.NewTextArea(),
		scopedOrder:           []string{},
		scopedEnabled:         map[string]bool{},
		sessionSortRecent:     true,
		sessionNamedOnly:      false,
		sessionShowPath:       false,
		authWorking:           false,
		toolsExpanded:         false,
		hideThinking:          false,
		lastEscape:            time.Time{},
		lastControlC:          time.Time{},
		escapePresses:         0,
		working:               false,
		compacting:            false,
		workStartedAt:         time.Time{},
		workFrame:             0,
		scrollOffset:          0,
		selection:             emptyMouseSelection(),
		streamedToolEvents:    0,
		promptHistoryIndex:    0,
		promptSequence:        0,
		statusMessage:         "",
		tokenUsage:            model.EmptyTokenUsage(),
		selectedPanelKind:     "",
		streamingText:         "",
		streamingThinkingText: "",
		extensionUI:           extui.NewState(),
	}
	app.addWelcomeMessage()

	return app
}

func initialAppTheme(options *RunOptions) terminalTheme {
	if options.Config != nil && options.Config.App.Env == "test" {
		return darkTheme()
	}

	return themeByName("dark")
}

func initialResourceSnapshot(options *RunOptions) core.ResourceSnapshot {
	resources := core.ResourceSnapshot{
		SkillDiagnostics:  nil,
		AgentInstructions: "",
		Skills:            nil,
	}
	if options.Resources != nil {
		resources = *options.Resources
	}

	return resources
}

func (app *App) loop(ctx context.Context) {
	defer app.stopAgentTaskWatches()

	workTicker := time.NewTicker(workFrameInterval)
	defer workTicker.Stop()

	frameTicker := time.NewTicker(streamingFrameInterval)
	defer frameTicker.Stop()

	extensionTimer := time.NewTimer(time.Hour)

	stopTimer(extensionTimer)
	defer extensionTimer.Stop()

	messageWarmTimer := time.NewTimer(time.Hour)

	stopTimer(messageWarmTimer)
	defer messageWarmTimer.Stop()

	dirty := true
	for {
		dirty = app.drawDirtyFrame(ctx, dirty)

		shouldQuit, nextDirty := app.runLoopStep(ctx, workTicker, frameTicker, extensionTimer, messageWarmTimer, dirty)
		if shouldQuit {
			return
		}

		dirty = nextDirty
	}
}

func (app *App) drawDirtyFrame(ctx context.Context, dirty bool) bool {
	if dirty && !app.throttleDraws() {
		app.draw(ctx)

		return false
	}

	return dirty
}

func (app *App) runLoopStep(
	ctx context.Context,
	workTicker *time.Ticker,
	frameTicker *time.Ticker,
	extensionTimer *time.Timer,
	messageWarmTimer *time.Timer,
	dirty bool,
) (shouldQuit, nextDirty bool) {
	select {
	case event := <-app.screen.EventQ():
		return app.handleLoopEvent(ctx, event)
	case <-app.workTick(workTicker):
		app.workFrame++
		if time.Since(app.agentTasksRefreshedAt) >= agentTaskRefreshInterval {
			app.refreshVisibleAgentTasks(ctx)
			app.refreshAgentTasksPanel(ctx)
		}

		app.emitExtensionRuntimeEventOrMessage(ctx, extensionEventTick, map[string]any{})

		return false, true
	case <-app.frameTick(frameTicker, dirty):
		if dirty {
			app.emitExtensionRuntimeEventOrMessage(ctx, extensionEventTick, map[string]any{})
		}

		app.draw(ctx)

		return false, false
	case <-app.extensionTimerTick(extensionTimer):
		app.emitExtensionRuntimeEventOrMessage(ctx, extensionEventTick, map[string]any{})

		return false, true
	case <-app.messageCacheWarmTick(messageWarmTimer):
		app.transcript.LineCache.queued = false
		app.transcript.LineCache.warmStep(app)

		return false, true
	}
}

func (app *App) handleLoopEvent(ctx context.Context, event tcell.Event) (shouldQuit, dirty bool) {
	if event == nil {
		return true, false
	}

	if resize, ok := event.(*tcell.EventResize); ok {
		return app.drawLatestResize(ctx, resize)
	}

	if delta, ok := app.scrollDeltaForEvent(event); ok {
		return app.handleScrollLoopEvent(ctx, delta)
	}

	shouldQuit, err := app.handleEvent(ctx, event)
	if err != nil {
		app.addMessage(transcript.RoleCustom, err.Error())
	}

	if shouldQuit {
		return true, false
	}

	if app.shouldDrawImmediately(event) {
		app.draw(ctx)

		return false, false
	}

	return false, true
}

func (app *App) handleScrollLoopEvent(ctx context.Context, delta int) (shouldQuit, dirty bool) {
	coalesced := app.coalesceScrollEvents(delta)
	app.scrollTranscript(coalesced.Delta)
	app.draw(ctx)

	if coalesced.Pending != nil {
		return app.handleLoopEvent(ctx, coalesced.Pending)
	}

	return false, false
}

func (app *App) drawLatestResize(ctx context.Context, resize *tcell.EventResize) (shouldQuit, dirty bool) {
	pending := app.coalesceResizeEvents(resize)
	if pending.Resize != nil {
		if err := app.applyResizeEvent(ctx, pending.Resize); err != nil {
			app.addMessage(transcript.RoleCustom, err.Error())
		}
	} else if pending.Event != nil {
		shouldQuit, _ = app.handleLoopEvent(ctx, pending.Event)
		if shouldQuit {
			return true, false
		}
	}

	app.draw(ctx)

	return false, false
}

func (app *App) coalesceResizeEvents(resize *tcell.EventResize) resizeCoalescedEvent {
	latest := resize

	for {
		select {
		case event := <-app.screen.EventQ():
			nextResize, matched := event.(*tcell.EventResize)
			if !matched {
				app.lastResize = latest

				return resizeCoalescedEvent{Resize: nil, Event: event}
			}

			latest = nextResize
		default:
			app.lastResize = latest

			return resizeCoalescedEvent{Resize: latest, Event: nil}
		}
	}
}

func (app *App) workTick(ticker *time.Ticker) <-chan time.Time {
	if app.busy() || app.hasRunningAgentTasks() || app.selectedPanelKind == panelAgentTasks {
		return ticker.C
	}

	return nil
}

func (app *App) frameTick(ticker *time.Ticker, dirty bool) <-chan time.Time {
	if app.throttleDraws() || dirty {
		return ticker.C
	}

	return nil
}

func (app *App) messageCacheWarmTick(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}

	if app.transcript.LineCache.warm || app.busy() || app.scrollOffset != 0 || app.toolsExpanded {
		app.transcript.LineCache.queued = false

		stopTimer(timer)

		return nil
	}

	if len(app.transcript.History) == 0 || app.transcript.LastMaxRows <= 0 {
		app.transcript.LineCache.queued = false

		stopTimer(timer)

		return nil
	}

	if !app.transcript.LineCache.queued {
		resetTimer(timer, 1*time.Millisecond)

		app.transcript.LineCache.queued = true
	}

	return timer.C
}

func (app *App) extensionTimerTick(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}

	scheduler, hasScheduler := app.extensions.(extension.TimerScheduler)
	if !hasScheduler {
		stopTimer(timer)

		return nil
	}

	delay, hasTimer := scheduler.NextTimerDelay(time.Now())
	if !hasTimer {
		stopTimer(timer)

		return nil
	}

	resetTimer(timer, delay)

	return timer.C
}

func resetTimer(timer *time.Timer, delay time.Duration) {
	stopTimer(timer)
	timer.Reset(delay)
}

func stopTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}

	select {
	case <-timer.C:
	default:
	}
}

func (app *App) throttleDraws() bool {
	return app.busy()
}

func (app *App) busy() bool {
	return app.working || app.authWorking || app.compacting
}

func (app *App) shouldDrawImmediately(event tcell.Event) bool {
	interrupt, matched := event.(*tcell.EventInterrupt)
	if !matched {
		return true
	}

	payload, matched := interrupt.Data().(*asyncEvent)
	if !matched {
		return true
	}

	return !isHighVolumePromptStreamEvent(payload.Kind)
}

func isHighVolumePromptStreamEvent(kind asyncEventKind) bool {
	switch kind {
	case asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot:
		return true
	case asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventAgentTaskCompleted,
		asyncEventCompactStart,
		asyncEventCompactDone,
		asyncEventCompactError,
		asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptRetry,
		asyncEventPromptError,
		asyncEventPromptContext,
		asyncEventAgentTaskChanged:
		return false
	}

	return false
}

func (app *App) loadInitialMessages(ctx context.Context) error {
	messages, err := app.sessionMessages(ctx, app.sessionID)
	if err != nil {
		return err
	}

	app.appendSessionMessages(messages)

	return nil
}

func (app *App) sessionMessages(ctx context.Context, sessionID string) ([]database.SessionMessageEntity, error) {
	if sessionID == "" || app.runtime == nil {
		return nil, nil
	}

	messages, err := app.runtime.SessionRepository().TranscriptMessages(ctx, sessionID)
	if err != nil {
		return nil, terminalError(err, "load initial messages")
	}

	return messages, nil
}

func (app *App) appendSessionMessages(messages []database.SessionMessageEntity) {
	for index := range messages {
		message := &messages[index]
		app.appendMessage(chatMessage{
			CreatedAt: message.CreatedAt,
			Role:      transcript.FromDatabaseRole(message.Role),
			Content:   message.Content,
		})

		if message.Role == database.RoleUser {
			app.recordPromptHistory(message.Content)
		}
	}
}

func (app *App) addSystemMessage(content string) {
	app.addMessage(transcript.RoleCustom, content)
}

func (app *App) addMessage(role transcript.Role, content string) {
	app.appendMessage(newChatMessage(role, content))
}

func newChatMessage(role transcript.Role, content string) chatMessage {
	return chatMessage{CreatedAt: time.Now().UTC(), Role: role, Content: content}
}

func emptyCachedRenderedMessage() cachedRenderedMessage {
	var message cachedRenderedMessage

	return message
}

func (app *App) appendMessage(message chatMessage) {
	app.transcript.History = append(app.transcript.History, message)
	app.transcript.LineCache.appendInvalidation()
}

func (app *App) resetMessages() {
	app.transcript.History = []chatMessage{}
	app.transcript.LineCache.reset()
	app.tokenUsage = model.EmptyTokenUsage()
	app.resetPromptHistory()
}

func (app *App) truncateMessages(length int) {
	app.transcript.History = app.transcript.History[:length]
	app.transcript.LineCache.truncate(length)
	app.tokenUsage = model.EmptyTokenUsage()
}

func (app *App) resetStreamingBlocks() {
	app.transcript.Streaming.Blocks = nil
	app.transcript.Streaming.LineCache = nil
	app.runningToolBlocks = nil
}

func (app *App) setStatus(message string) {
	app.statusMessage = message
}

func (app *App) setModel(provider, modelID string) {
	app.setModelSelection(provider, modelID)
	app.addSystemMessage("model selected: " + provider + "/" + modelID)
}

func (app *App) setThinkingLevel(level string) {
	app.setThinkingLevelValue(level)
	app.setStatus("thinking: " + level)
}

func modelLabel(provider, modelID string) string {
	if provider == "" {
		return modelID
	}

	return provider + "/" + modelID
}

func trimCommandPrefix(text string) string {
	return strings.TrimSpace(strings.TrimPrefix(text, "/"))
}
