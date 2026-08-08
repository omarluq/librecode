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

type workflowInspector interface {
	Get(context.Context, string) (*database.WorkflowRunEntity, bool, error)
	List(context.Context, string, int) ([]database.WorkflowRunEntity, error)
	Events(context.Context, string, int64, int) ([]database.TaskEventEntity, error)
	AgentTasks(context.Context, string) ([]database.WorkflowAgentTaskEntity, error)
	AgentTask(context.Context, string) (*database.AgentTaskEntity, bool, error)
	AgentTaskDetails(context.Context, []string) ([]database.WorkflowAgentTaskDetail, error)
	Cancel(context.Context, string, string) (bool, error)
}

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
	Attachments *attachmentSummaries
	CreatedAt   time.Time
	Role        transcript.Role
	Content     string
}

type activePromptState struct {
	Cancel        context.CancelFunc
	ParentEntryID *string
	SessionID     string
	UserEntryID   string
	Prompt        string
	Images        []imageAttachment
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

type markdownListItemRange struct {
	StartLine int
	EndLine   int
}

type cachedRenderedMessage struct {
	Lines     []tui.Line
	ListItems []markdownListItemRange
	Valid     bool
}

type transcriptListSelection struct {
	MessageIndex int
	ItemIndex    int
	Active       bool
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
	Workflows  workflowInspector             `json:"-"`
	Settings   *database.DocumentRepository  `json:"-"`
	Models     *model.Registry               `json:"-"`
	Auth       *auth.Storage                 `json:"-"`
	Config     *config.Config                `json:"-"`
	CWD        string                        `json:"cwd"`
	SessionID  string                        `json:"session_id"`
}

// App is the terminal chat UI.
type App struct {
	extensionUI               extui.State
	lastControlC              time.Time
	workStartedAt             time.Time
	agentTasksRefreshedAt     time.Time
	lastEscape                time.Time
	workflows                 workflowInspector
	screen                    terminalScreen
	extensions                extension.TerminalEventRunner
	systemClipboard           systemClipboardWriter
	imageClipboard            systemClipboardImageReader
	activeCompaction          *activeCompactionState
	activePrompt              *activePromptState
	settings                  *database.DocumentRepository
	models                    *model.Registry
	auth                      *auth.Storage
	cfg                       *config.Config
	keys                      *keybindings
	panel                     *panel.Model
	pendingParentID           *string
	agentTaskWatches          map[string]context.CancelFunc
	workflowSteps             map[string][]database.WorkflowAgentTaskDetail
	scopedEnabled             map[string]bool
	lastResize                *tcell.EventResize
	frame                     *tui.CellBuffer
	workflowProgress          map[string]workflowProgress
	runtime                   *assistant.Runtime
	sessionViews              map[string]sessionViewState
	deliveredAgentTasks       map[string]struct{}
	deliveredToolTasks        map[string]struct{}
	cancelToolTaskCompletions context.CancelFunc
	renderer                  *tui.Renderer
	theme                     terminalTheme
	sessionID                 string
	streamingThinkingText     string
	cwd                       string
	promptHistoryDraft        string
	promptHistoryDraftImages  []imageAttachment
	mode                      appMode
	streamingText             string
	statusMessage             string
	agentTaskSummaryOwnerID   string
	workflowPanelRunID        string
	workflowSummaryRunID      string
	selectedPanelKind         panel.Kind
	resources                 core.ResourceSnapshot
	runningToolBlocks         []runningToolBlock
	composerImages            []imageAttachment
	agentTasks                []database.AgentTaskEntity
	toolTasks                 []database.ToolTaskEntity
	liveAgentCompletions      []chatMessage
	activeWorkflows           []database.WorkflowRunEntity
	agentTaskSessionStack     []string
	queuedMessages            []promptDraft
	hiddenQueuedMessages      []promptDraft
	promptHistory             []string
	promptHistoryImages       [][]imageAttachment
	scopedOrder               []string
	composerBuffer            tui.TextArea
	transcript                transcriptState
	tokenUsage                model.TokenUsage
	selection                 mouseSelection
	transcriptList            transcriptListSelection
	agentTaskSummarySelection agentTaskSummarySelection
	promptSequence            uint64
	autocompleteSelection     int
	workFrame                 int
	streamedToolEvents        int
	escapePresses             int
	promptHistoryIndex        int
	scrollOffset              int
	sessionNamedOnly          bool
	autocompleteClosed        bool
	bracketedPaste            bool
	hideThinking              bool
	working                   bool
	compacting                bool
	toolsExpanded             bool
	authWorking               bool
	sessionShowPath           bool
	sessionSortRecent         bool
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

	screen.EnablePaste()
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
	app.logToolTaskRefreshError(ctx, app.refreshToolTasks(ctx))
	app.watchToolTaskCompletions(ctx)
	app.loop(ctx)

	return nil
}

type terminalScreen interface {
	tui.ContentSetter
	EventQ() chan tcell.Event
	HideCursor()
	Show()
	SetClipboard(data []byte)
	EnablePaste()
	ShowCursor(x, y int)
	Size() (width, height int)
}

func newApp(screen terminalScreen, options *RunOptions) *App {
	app := newAppState(screen, options)
	app.addWelcomeMessage()

	return app
}

func newAppState(screen terminalScreen, options *RunOptions) *App {
	return &App{
		screen:                  screen,
		renderer:                tui.NewRenderer(screen),
		frame:                   nil,
		lastResize:              nil,
		systemClipboard:         newDesktopClipboard(),
		imageClipboard:          newDesktopClipboard(),
		runtime:                 options.Runtime,
		workflows:               options.Workflows,
		extensions:              options.Extensions,
		settings:                options.Settings,
		models:                  options.Models,
		auth:                    options.Auth,
		cfg:                     options.Config,
		keys:                    newDefaultKeybindings(),
		theme:                   initialAppTheme(options),
		resources:               initialResourceSnapshot(options),
		mode:                    modeChat,
		panel:                   nil,
		cwd:                     options.CWD,
		sessionID:               options.SessionID,
		sessionViews:            map[string]sessionViewState{},
		agentTaskSessionStack:   []string{},
		pendingParentID:         nil,
		activePrompt:            nil,
		activeCompaction:        nil,
		transcript:              initialTranscriptState(),
		runningToolBlocks:       []runningToolBlock{},
		liveAgentCompletions:    []chatMessage{},
		agentTasks:              []database.AgentTaskEntity{},
		toolTasks:               []database.ToolTaskEntity{},
		activeWorkflows:         []database.WorkflowRunEntity{},
		workflowProgress:        map[string]workflowProgress{},
		workflowSteps:           map[string][]database.WorkflowAgentTaskDetail{},
		workflowSummaryRunID:    "",
		workflowPanelRunID:      "",
		agentTaskSummaryOwnerID: "",
		agentTasksRefreshedAt:   time.Time{},
		agentTaskWatches:        map[string]context.CancelFunc{},
		deliveredAgentTasks:     map[string]struct{}{}, deliveredToolTasks: map[string]struct{}{},
		cancelToolTaskCompletions: nil, queuedMessages: []promptDraft{},
		hiddenQueuedMessages:      []promptDraft{},
		promptHistory:             []string{},
		promptHistoryImages:       [][]imageAttachment{},
		promptHistoryDraft:        "",
		promptHistoryDraftImages:  nil,
		autocompleteSelection:     0,
		autocompleteClosed:        false,
		composerBuffer:            tui.NewTextArea(),
		composerImages:            []imageAttachment{},
		bracketedPaste:            false,
		scopedOrder:               []string{},
		scopedEnabled:             map[string]bool{},
		sessionSortRecent:         true,
		sessionNamedOnly:          false,
		sessionShowPath:           false,
		authWorking:               false,
		toolsExpanded:             false,
		hideThinking:              false,
		lastEscape:                time.Time{},
		lastControlC:              time.Time{},
		escapePresses:             0,
		working:                   false,
		compacting:                false,
		workStartedAt:             time.Time{},
		workFrame:                 0,
		scrollOffset:              0,
		selection:                 emptyMouseSelection(),
		transcriptList:            emptyTranscriptListSelection(),
		agentTaskSummarySelection: agentTaskSummarySelection{ItemIndex: 0, Active: false},
		streamedToolEvents:        0,
		promptHistoryIndex:        0,
		promptSequence:            0,
		statusMessage:             "",
		tokenUsage:                model.EmptyTokenUsage(),
		selectedPanelKind:         "",
		streamingText:             "",
		streamingThinkingText:     "",
		extensionUI:               extui.NewState(),
	}
}

func initialTranscriptState() transcriptState {
	return transcriptState{
		History: []chatMessage{},
		Streaming: transcriptStreamingState{
			Blocks:     []chatMessage{},
			LineCache:  nil,
			CacheState: emptyMessageLineCacheState(),
		},
		LineCache:   emptyMessageLineCache(),
		LastMaxRows: 0,
	}
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
	defer app.stopToolTaskCompletions()

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
	if ctx.Err() != nil {
		return true, false
	}

	select {
	case <-ctx.Done():
		return true, false
	case event := <-app.screen.EventQ():
		return app.handleLoopEvent(ctx, event)
	case <-app.workTick(workTicker):
		app.workFrame++
		if time.Since(app.agentTasksRefreshedAt) >= agentTaskRefreshInterval {
			app.refreshVisibleAgentTasks(ctx)
			app.logToolTaskRefreshError(ctx, app.refreshToolTasks(ctx))
			app.refreshAgentTasksPanel(ctx)
			app.refreshWorkflowsPanel(ctx)
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
	if app.busy() || app.hasRunningAgentTasks() || app.hasRunningToolTasks() ||
		app.selectedPanelKind == panelAgentTasks || app.selectedPanelKind == panelWorkflows {
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
		asyncEventPromptUsageSnapshot,
		asyncEventAgentTaskStream:
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
		asyncEventAgentTaskChanged,
		asyncEventAgentTaskReplayError:
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
			CreatedAt:   message.CreatedAt,
			Role:        transcript.FromDatabaseRole(message.Role),
			Content:     message.Content,
			Attachments: databaseAttachmentSummaries(message.Parts),
		})

		if message.Role == database.RoleUser {
			app.recordPromptDraftHistory(promptDraft{
				Text: message.Content, Images: imageAttachmentsFromDatabase(message.Parts),
			})
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
	return chatMessage{CreatedAt: time.Now().UTC(), Role: role, Content: content, Attachments: nil}
}

func emptyCachedRenderedMessage() cachedRenderedMessage {
	var message cachedRenderedMessage

	return message
}

func (app *App) appendMessage(message chatMessage) {
	app.blurTranscriptList()
	app.transcript.History = append(app.transcript.History, message)
	app.transcript.LineCache.appendInvalidation()
}

func (app *App) resetMessages() {
	app.blurTranscriptList()
	app.scrollOffset = 0
	app.transcript.History = []chatMessage{}
	app.transcript.LineCache.reset()
	app.tokenUsage = model.EmptyTokenUsage()
	app.resetPromptHistory()
}

func (app *App) truncateMessages(length int) {
	app.blurTranscriptList()
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
