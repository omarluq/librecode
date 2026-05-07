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
	"github.com/omarluq/librecode/internal/model"
)

const (
	defaultEditorRows      = 6
	workFrameInterval      = 120 * time.Millisecond
	streamingFrameInterval = 33 * time.Millisecond
	doubleEscapeDelay      = 500 * time.Millisecond
	doubleControlCDelay    = 2 * time.Second
)

type appMode string

const (
	modeChat  appMode = "chat"
	modePanel appMode = "panel"
)

type chatMessage struct {
	CreatedAt time.Time
	Role      database.Role
	Content   string
}

type activePromptState struct {
	Cancel           context.CancelFunc
	ParentEntryID    *string
	SessionID        string
	UserEntryID      string
	Prompt           string
	ID               uint64
	BaselineMessages int
	Canceled         bool
}

type messageLineCacheState struct {
	ThemeName     string
	Width         int
	HideThinking  bool
	ToolsExpanded bool
}

type cachedRenderedMessage struct {
	Lines []styledLine
	Valid bool
}

// RunOptions configures the terminal app.
type RunOptions struct {
	Resources *core.ResourceSnapshot       `json:"resources"`
	Runtime   *assistant.Runtime           `json:"-"`
	Settings  *database.DocumentRepository `json:"-"`
	Models    *model.Registry              `json:"-"`
	Auth      *auth.Storage                `json:"-"`
	Config    *config.Config               `json:"-"`
	CWD       string                       `json:"cwd"`
	SessionID string                       `json:"session_id"`
}

// App is the terminal chat UI.
type App struct {
	lastEscape                   time.Time
	lastControlC                 time.Time
	screen                       tcell.Screen
	renderer                     *screenRenderer
	frame                        *cellBuffer
	runtime                      *assistant.Runtime
	settings                     *database.DocumentRepository
	models                       *model.Registry
	auth                         *auth.Storage
	cfg                          *config.Config
	editor                       *editor
	keys                         *keybindings
	panel                        *selectionPanel
	pendingParentID              *string
	activePrompt                 *activePromptState
	canceledPrompts              map[uint64]*activePromptState
	scopedEnabled                map[string]bool
	theme                        terminalTheme
	mode                         appMode
	cwd                          string
	sessionID                    string
	statusMessage                string
	selectedPanelKind            panelKind
	streamingText                string
	streamingThinkingText        string
	streamingBlocks              []chatMessage
	streamingBlockLineCache      []cachedRenderedMessage
	streamingBlockLineCacheState messageLineCacheState
	resources                    core.ResourceSnapshot
	queuedMessages               []string
	messages                     []chatMessage
	messageLineCache             []cachedRenderedMessage
	messageLineCacheState        messageLineCacheState
	scopedOrder                  []string
	sessionSortRecent            bool
	sessionNamedOnly             bool
	sessionShowPath              bool
	authWorking                  bool
	toolsExpanded                bool
	hideThinking                 bool
	working                      bool
	workFrame                    int
	scrollOffset                 int
	streamedToolEvents           int
	promptSequence               uint64
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
	screen.EnableMouse(tcell.MouseButtonEvents)
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
	app.loop(ctx)

	return nil
}

func newApp(screen tcell.Screen, options *RunOptions) *App {
	appTheme := themeByName("dark")
	if options.Config != nil && options.Config.App.Env == "test" {
		appTheme = darkTheme()
	}
	resources := core.ResourceSnapshot{
		SkillDiagnostics:   nil,
		PromptDiagnostics:  nil,
		AppendSystemPrompt: nil,
		ContextFiles:       nil,
		SystemPrompt:       "",
		Skills:             nil,
		Prompts:            nil,
	}
	if options.Resources != nil {
		resources = *options.Resources
	}
	app := &App{
		screen:                       screen,
		renderer:                     newScreenRenderer(screen),
		frame:                        nil,
		runtime:                      options.Runtime,
		settings:                     options.Settings,
		models:                       options.Models,
		auth:                         options.Auth,
		cfg:                          options.Config,
		editor:                       newEditor(),
		keys:                         newDefaultKeybindings(),
		theme:                        appTheme,
		resources:                    resources,
		mode:                         modeChat,
		panel:                        nil,
		cwd:                          options.CWD,
		sessionID:                    options.SessionID,
		pendingParentID:              nil,
		activePrompt:                 nil,
		canceledPrompts:              map[uint64]*activePromptState{},
		messages:                     []chatMessage{},
		messageLineCache:             nil,
		messageLineCacheState:        messageLineCacheState{},
		queuedMessages:               []string{},
		scopedOrder:                  []string{},
		scopedEnabled:                map[string]bool{},
		sessionSortRecent:            true,
		sessionNamedOnly:             false,
		sessionShowPath:              false,
		authWorking:                  false,
		toolsExpanded:                false,
		hideThinking:                 false,
		lastEscape:                   time.Time{},
		lastControlC:                 time.Time{},
		working:                      false,
		workFrame:                    0,
		scrollOffset:                 0,
		streamedToolEvents:           0,
		promptSequence:               0,
		statusMessage:                "",
		selectedPanelKind:            "",
		streamingText:                "",
		streamingThinkingText:        "",
		streamingBlocks:              []chatMessage{},
		streamingBlockLineCache:      nil,
		streamingBlockLineCacheState: messageLineCacheState{},
	}
	app.addWelcomeMessage()

	return app
}

func (app *App) loop(ctx context.Context) {
	workTicker := time.NewTicker(workFrameInterval)
	defer workTicker.Stop()
	frameTicker := time.NewTicker(streamingFrameInterval)
	defer frameTicker.Stop()
	dirty := true
	for {
		if dirty && !app.throttleDraws() {
			app.draw()
			dirty = false
		}
		var workTick <-chan time.Time
		if app.working || app.authWorking {
			workTick = workTicker.C
		}
		var frameTick <-chan time.Time
		if dirty && app.throttleDraws() {
			frameTick = frameTicker.C
		}
		select {
		case event := <-app.screen.EventQ():
			if event == nil {
				return
			}
			shouldQuit, err := app.handleEvent(ctx, event)
			if err != nil {
				app.addMessage(database.RoleCustom, err.Error())
			}
			if shouldQuit {
				return
			}
			dirty = true
			if app.shouldDrawImmediately(event) {
				app.draw()
				dirty = false
			}
		case <-workTick:
			app.workFrame++
			dirty = true
		case <-frameTick:
			app.draw()
			dirty = false
		}
	}
}

func (app *App) throttleDraws() bool {
	return app.working || app.authWorking
}

func (app *App) shouldDrawImmediately(event tcell.Event) bool {
	interrupt, ok := event.(*tcell.EventInterrupt)
	if !ok {
		return true
	}
	payload, ok := interrupt.Data().(asyncEvent)
	if !ok {
		return true
	}

	return !isHighVolumePromptStreamEvent(payload.Kind)
}

func isHighVolumePromptStreamEvent(kind asyncEventKind) bool {
	switch kind {
	case asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult:
		return true
	case asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptError:
		return false
	}

	return false
}

func (app *App) loadInitialMessages(ctx context.Context) error {
	if app.sessionID == "" || app.runtime == nil {
		return nil
	}
	messages, err := app.runtime.SessionRepository().Messages(ctx, app.sessionID)
	if err != nil {
		return err
	}
	for index := range messages {
		message := &messages[index]
		app.appendMessage(chatMessage{
			CreatedAt: message.CreatedAt,
			Role:      message.Role,
			Content:   message.Content,
		})
	}

	return nil
}

func (app *App) addSystemMessage(content string) {
	app.addMessage(database.RoleCustom, content)
}

func (app *App) addMessage(role database.Role, content string) {
	app.appendMessage(chatMessage{CreatedAt: time.Now().UTC(), Role: role, Content: content})
}

func (app *App) appendMessage(message chatMessage) {
	app.messages = append(app.messages, message)
}

func (app *App) resetMessages() {
	app.messages = []chatMessage{}
	app.messageLineCache = nil
}

func (app *App) truncateMessages(length int) {
	app.messages = app.messages[:length]
	if len(app.messageLineCache) > length {
		app.messageLineCache = app.messageLineCache[:length]
	}
}

func (app *App) resetStreamingBlocks() {
	app.streamingBlocks = nil
	app.streamingBlockLineCache = nil
}

func (app *App) setStatus(message string) {
	app.statusMessage = message
}

func (app *App) currentThinkingLevel() string {
	if app.cfg == nil || app.cfg.Assistant.ThinkingLevel == "" {
		return string(model.ThinkingOff)
	}

	return app.cfg.Assistant.ThinkingLevel
}

func (app *App) currentProvider() string {
	if app.cfg == nil {
		return "local"
	}

	return app.cfg.Assistant.Provider
}

func (app *App) currentModel() string {
	if app.cfg == nil {
		return "librecode"
	}

	return app.cfg.Assistant.Model
}

func (app *App) setModel(provider, modelID string) {
	if app.cfg != nil {
		app.cfg.Assistant.Provider = provider
		app.cfg.Assistant.Model = modelID
	}
	app.persistSessionSettings()
	app.addSystemMessage("model selected: " + provider + "/" + modelID)
}

func (app *App) setThinkingLevel(level string) {
	if app.cfg != nil {
		app.cfg.Assistant.ThinkingLevel = level
	}
	app.persistSessionSettings()
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
