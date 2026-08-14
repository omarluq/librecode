// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"strings"
	"time"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
)

func (runtime *Runtime) appendUserPromptEntry(
	ctx context.Context,
	sessionID string,
	parentID *string,
	prompt string,
	images []ImageAttachment,
	display bool,
) (*database.EntryEntity, error) {
	parts := make([]database.MessagePartEntity, 0, len(images)+1)

	content := prompt
	if strings.TrimSpace(prompt) != "" {
		parts = append(parts, database.MessagePartEntity{
			Text: prompt, MIMEType: "", Name: "", Type: database.MessagePartText,
			Data: nil, Width: 0, Height: 0,
		})
	} else {
		content = ""
	}

	for index := range images {
		image := &images[index]
		parts = append(parts, database.MessagePartEntity{
			Text: "", MIMEType: image.MIMEType, Name: image.Name, Type: database.MessagePartImage,
			Data: append([]byte(nil), image.Data...), Width: image.Width, Height: image.Height,
		})
	}

	message := database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: content,
		Provider: "", Model: "", Parts: parts,
	}
	modelFacing := len(images) > 0 || promptModelFacing(prompt)

	entry, err := runtime.sessions.AppendMessageWithDisplay(
		ctx,
		sessionID,
		parentID,
		&message,
		&modelFacing,
		&display,
	)

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
		Model:     runtime.cfg.Assistant.Model, Parts: nil,
	}

	persistedUsage := bundle.ProviderUsage
	persistedUsage.ContextWindow = bundle.Usage.ContextWindow
	persistedUsage.ContextTokens = bundle.Usage.ContextTokens

	entry, err := runtime.sessions.AppendMessageWithMetadata(
		ctx,
		sessionID,
		parentID,
		&message,
		&bundle.ModelFacing,
		contextwindow.ProviderUsageEntity(&persistedUsage),
	)

	return entry, assistantError(err, "append assistant response")
}
