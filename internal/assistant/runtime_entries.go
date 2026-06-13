// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"time"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
)

func (runtime *Runtime) appendUserPromptEntry(
	ctx context.Context,
	sessionID string,
	parentID *string,
	prompt string,
) (*database.EntryEntity, error) {
	message := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   prompt,
		Provider:  "",
		Model:     "",
	}
	modelFacing := promptModelFacing(prompt)

	entry, err := runtime.sessions.AppendMessageWithModelFacing(ctx, sessionID, parentID, &message, &modelFacing)

	return entry, assistantError(err, "append model-facing message")
}

func (runtime *Runtime) appendAssistantResponseEntry(
	ctx context.Context,
	sessionID string,
	parentID *string,
	bundle *responseBundle,
) (*database.EntryEntity, error) {
	message := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleAssistant,
		Content:   bundle.Text,
		Provider:  runtime.cfg.Assistant.Provider,
		Model:     runtime.cfg.Assistant.Model,
	}

	entry, err := runtime.sessions.AppendMessageWithMetadata(
		ctx,
		sessionID,
		parentID,
		&message,
		&bundle.ModelFacing,
		contextwindow.ProviderUsageEntity(bundle.Usage),
	)

	return entry, assistantError(err, "append assistant response")
}
