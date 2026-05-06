package terminal

import (
	"context"
	"fmt"
	"strings"

	"github.com/omarluq/librecode/internal/database"
)

func (app *App) submitCommand(ctx context.Context, text string) (bool, error) {
	command, args, _ := strings.Cut(trimCommandPrefix(text), " ")
	if command == "quit" || command == "exit" {
		return true, nil
	}
	if app.openCommandPanel(ctx, command) {
		return false, nil
	}

	return app.runSessionCommand(ctx, command, strings.TrimSpace(args), text)
}

func (app *App) openCommandPanel(ctx context.Context, command string) bool {
	switch command {
	case "hotkeys":
		app.openHotkeysPanel()
	case "model":
		app.openModelPanel()
	case "settings":
		app.openSettingsPanel()
	case "resume":
		app.openSessionPanel(ctx)
	case "tree":
		app.openTreePanel(ctx)
	case "changelog":
		app.openChangelogPanel()
	default:
		return false
	}

	return true
}

func (app *App) runSessionCommand(ctx context.Context, command, args, original string) (bool, error) {
	switch command {
	case "new":
		return false, app.newSession(ctx, args)
	case "name":
		return false, app.renameSession(ctx, args)
	case "session":
		app.showSessionInfo(ctx)
		return false, nil
	default:
		return false, app.sendPrompt(ctx, original)
	}
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
