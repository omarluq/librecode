// Package terminal implements a librecode-style interactive terminal UI.
package terminal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const (
	defaultEditorRows = 6
	doubleEscapeDelay = 500 * time.Millisecond
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

// RunOptions configures the terminal app.
type RunOptions struct {
	Resources *core.ResourceSnapshot `json:"resources"`
	Runtime   *assistant.Runtime     `json:"-"`
	Models    *model.Registry        `json:"-"`
	Config    *config.Config         `json:"-"`
	CWD       string                 `json:"cwd"`
	SessionID string                 `json:"session_id"`
}

// App is the terminal chat UI.
type App struct {
	lastEscape        time.Time
	screen            tcell.Screen
	runtime           *assistant.Runtime
	models            *model.Registry
	cfg               *config.Config
	editor            *editor
	keys              *keybindings
	panel             *selectionPanel
	pendingParentID   *string
	scopedEnabled     map[string]bool
	theme             terminalTheme
	mode              appMode
	cwd               string
	sessionID         string
	statusMessage     string
	selectedPanelKind panelKind
	resources         core.ResourceSnapshot
	queuedMessages    []string
	messages          []chatMessage
	scopedOrder       []string
	toolsExpanded     bool
	hideThinking      bool
	working           bool
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
	defer screen.Fini()

	app := newApp(screen, options)
	if err := app.loadInitialMessages(ctx); err != nil {
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
		screen:            screen,
		runtime:           options.Runtime,
		models:            options.Models,
		cfg:               options.Config,
		editor:            newEditor(),
		keys:              newDefaultKeybindings(),
		theme:             appTheme,
		resources:         resources,
		mode:              modeChat,
		panel:             nil,
		cwd:               options.CWD,
		sessionID:         options.SessionID,
		pendingParentID:   nil,
		messages:          []chatMessage{},
		queuedMessages:    []string{},
		scopedOrder:       []string{},
		scopedEnabled:     map[string]bool{},
		toolsExpanded:     false,
		hideThinking:      false,
		lastEscape:        time.Time{},
		working:           false,
		statusMessage:     "",
		selectedPanelKind: "",
	}
	app.addSystemMessage("librecode • librecode-style TUI. Type /hotkeys for shortcuts or /quit to exit.")

	return app
}

func (app *App) loop(ctx context.Context) {
	for {
		app.draw()
		event := <-app.screen.EventQ()
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
	}
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
		app.messages = append(app.messages, chatMessage{
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
	app.messages = append(app.messages, chatMessage{CreatedAt: time.Now().UTC(), Role: role, Content: content})
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
	app.addSystemMessage("model selected: " + provider + "/" + modelID)
}

func (app *App) setThinkingLevel(level string) {
	if app.cfg != nil {
		app.cfg.Assistant.ThinkingLevel = level
	}
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
