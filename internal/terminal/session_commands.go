package terminal

import (
	"context"
	"errors"
	"strings"

	"github.com/omarluq/librecode/internal/database"
)

func (app *App) cloneSession(ctx context.Context, name string) error {
	if app.sessionID == "" {
		return errors.New("no active session")
	}
	createdSession, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, name, app.sessionID)
	if err != nil {
		return err
	}
	app.sessionID = createdSession.ID
	app.pendingParentID = nil
	app.addSystemMessage("cloned session handle: " + createdSession.ID)

	return nil
}

func (app *App) copyLastAssistantMessage(ctx context.Context) error {
	message, ok, err := app.lastAssistantMessage(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no assistant message to copy")
	}
	app.copyTextToClipboard(message.Content)
	app.setStatus("copied last assistant message")

	return nil
}

func (app *App) lastAssistantMessage(ctx context.Context) (*database.SessionMessageEntity, bool, error) {
	if app.sessionID == "" {
		return nil, false, nil
	}
	messages, err := app.runtime.SessionRepository().Messages(ctx, app.sessionID)
	if err != nil {
		return nil, false, err
	}
	for offset := range len(messages) {
		index := len(messages) - 1 - offset
		message := &messages[index]
		if message.Role == database.RoleAssistant && strings.TrimSpace(message.Content) != "" {
			return message, true, nil
		}
	}

	return nil, false, nil
}

func (app *App) copyTextToClipboard(text string) {
	copyTextToClipboard(app.screen, text)
}
