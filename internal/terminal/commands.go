package terminal

import (
	"context"
	"fmt"
	"strings"

	"github.com/omarluq/librecode/internal/database"
)

const (
	commandLogin  = "login"
	commandLogout = "logout"
)

func (app *App) submitCommand(ctx context.Context, text string) (bool, error) {
	command, args, _ := strings.Cut(trimCommandPrefix(text), " ")
	trimmedArgs := strings.TrimSpace(args)
	if command == "quit" || command == "exit" {
		return true, nil
	}
	if trimmedArgs == "" && app.openCommandPanel(ctx, command) {
		return false, nil
	}

	return app.runSessionCommand(ctx, command, trimmedArgs, text)
}

func (app *App) openCommandPanel(ctx context.Context, command string) bool {
	handlers := map[string]func(){
		"changelog":     app.openChangelogPanel,
		"hotkeys":       app.openHotkeysPanel,
		commandLogin:    app.openLoginPanel,
		commandLogout:   app.openLogoutPanel,
		"model":         app.openModelPanel,
		"scoped-models": app.openScopedModelsPanel,
		"settings":      app.openSettingsPanel,
		"resume":        func() { app.openSessionPanel(ctx) },
		"tree":          func() { app.openTreePanel(ctx) },
	}
	handler, ok := handlers[command]
	if !ok {
		return false
	}
	handler()

	return true
}

func (app *App) runSessionCommand(ctx context.Context, command, args, original string) (bool, error) {
	if handler, ok := app.sessionCommandHandlers(ctx, args)[command]; ok {
		return false, handler()
	}
	if handler, ok := app.sessionCommandNotifications(ctx, command); ok {
		handler()
		return false, nil
	}

	app.sendPrompt(ctx, original)

	return false, nil
}

func (app *App) sessionCommandHandlers(ctx context.Context, args string) map[string]func() error {
	return map[string]func() error{
		"clone":       func() error { return app.cloneSession(ctx, args) },
		"copy":        func() error { return app.copyLastAssistantMessage(ctx) },
		"fork":        func() error { return app.newSession(ctx, args) },
		commandLogin:  func() error { return app.loginCommand(ctx, args) },
		commandLogout: func() error { return app.logoutCommand(ctx, args) },
		"name":        func() error { return app.renameSession(ctx, args) },
		"new":         func() error { return app.newSession(ctx, args) },
		"reload":      func() error { return app.reloadRuntime(ctx) },
	}
}

func (app *App) sessionCommandNotifications(ctx context.Context, command string) (func(), bool) {
	handlers := map[string]func(){
		"auth":    app.showAuthInfo,
		"compact": func() { app.addSystemMessage("manual compaction is not implemented yet") },
		"export":  func() { app.addSystemMessage("/" + command + " is not implemented yet") },
		"import":  func() { app.addSystemMessage("/" + command + " is not implemented yet") },
		"session": func() { app.showSessionInfo(ctx) },
		"share":   func() { app.addSystemMessage("/" + command + " is not implemented yet") },
	}
	handler, ok := handlers[command]

	return handler, ok
}

func (app *App) newSession(ctx context.Context, name string) error {
	createdSession, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, name, app.sessionID)
	if err != nil {
		return err
	}
	app.sessionID = createdSession.ID
	app.pendingParentID = nil
	app.messages = []chatMessage{}
	app.addSystemMessage("new session: " + createdSession.ID)

	return nil
}

func (app *App) renameSession(ctx context.Context, name string) error {
	if app.sessionID == "" {
		return fmt.Errorf("no active session")
	}
	if name == "" {
		return fmt.Errorf("name is required")
	}
	leaf, _, err := app.runtime.SessionRepository().LeafEntry(ctx, app.sessionID)
	if err != nil {
		return err
	}
	_, err = app.runtime.SessionRepository().AppendSessionInfo(ctx, app.sessionID, parentIDFromEntry(leaf), name)
	if err != nil {
		return err
	}
	app.setStatus("session named: " + name)

	return nil
}

func (app *App) showSessionInfo(ctx context.Context) {
	if app.sessionID == "" {
		app.addSystemMessage("session: none")
		return
	}
	entries, err := app.runtime.SessionRepository().Entries(ctx, app.sessionID)
	if err != nil {
		app.addSystemMessage(err.Error())
		return
	}
	messages, err := app.runtime.SessionRepository().Messages(ctx, app.sessionID)
	if err != nil {
		app.addSystemMessage(err.Error())
		return
	}
	content := strings.Join([]string{
		"session: " + app.sessionID,
		"cwd: " + app.cwd,
		"entries: " + intText(len(entries)),
		"messages: " + intText(len(messages)),
		"model: " + modelLabel(app.currentProvider(), app.currentModel()),
	}, "\n")
	app.addMessage(database.RoleCustom, content)
}

func parentIDFromEntry(entry *database.EntryEntity) *string {
	if entry == nil {
		return nil
	}

	return &entry.ID
}
