package terminal

import (
	"context"
	"fmt"
	"strings"

	"github.com/omarluq/librecode/internal/database"
)

func (app *App) cloneSession(ctx context.Context, name string) error {
	if app.sessionID == "" {
		return fmt.Errorf("no active session")
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
		return fmt.Errorf("no assistant message to copy")
	}
	if err := copyToClipboard(message.Content); err != nil {
		return err
	}
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
	for index := len(messages) - 1; index >= 0; index-- {
		message := &messages[index]
		if message.Role == database.RoleAssistant && strings.TrimSpace(message.Content) != "" {
			return message, true, nil
		}
	}

	return nil, false, nil
}

func copyToClipboard(_ string) error {
	return fmt.Errorf("copy to clipboard is not available in this terminal build")
}
