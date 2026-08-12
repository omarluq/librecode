package terminal

import (
	"context"
	"encoding/json"
	"time"

	"github.com/omarluq/librecode/internal/transcript"

	"github.com/omarluq/librecode/internal/assistant"
)

func (app *App) sendPrompt(ctx context.Context, text string) {
	app.sendDraft(ctx, promptDraft{Text: text, Images: nil}, true)
}

func (app *App) sendDraft(ctx context.Context, draft promptDraft, visible bool) {
	if app.busy() {
		app.queueDraft(draft, visible)

		return
	}

	promptCtx, cancel := context.WithCancel(ctx)
	parentEntryID := cloneStringPtr(app.pendingParentID)
	promptID := app.nextPromptID()
	request := &assistant.PromptRequest{
		OnEvent:          app.promptStreamHandler(promptCtx, promptID),
		OnRetry:          app.promptRetryHandler(promptCtx, promptID),
		OnUserEntry:      app.promptUserEntryHandler(ctx, promptID),
		OnSteeringReturn: app.steeringReturnHandler(ctx, promptID),
		ParentEntryID:    parentEntryID,
		SessionID:        app.sessionID,
		CWD:              app.cwd,
		Images:           draft.assistantImages(),
		Text:             draft.Text,
		Name:             "",
		ResumeLatest:     false,
		HideUserPrompt:   !visible,
	}
	app.pendingParentID = nil
	app.scrollOffset = 0
	app.streamingText = ""
	app.streamingThinkingText = ""
	app.resetStreamingBlocks()
	app.streamedToolEvents = 0

	userMessage := newChatMessage(transcript.RoleUser, draft.Text)
	userMessageTimestamp := int64(0)

	if visible {
		userMessage.Attachments = summarizeAttachments(draft.Images)
		userMessageTimestamp = userMessage.CreatedAt.UnixNano()
	}

	app.activePrompt = &activePromptState{
		Cancel:               cancel,
		SessionID:            app.sessionID,
		UserEntryID:          "",
		Prompt:               draft.Text,
		Images:               cloneImageAttachments(draft.Images),
		ID:                   promptID,
		UserMessageTimestamp: userMessageTimestamp,
		Canceled:             false,
	}
	if visible {
		app.appendMessage(userMessage)
	}

	app.working = true
	app.workStartedAt = time.Now()

	app.workFrame = 0
	go app.runPrompt(ctx, promptCtx, cancel, request, promptID)
}

func (app *App) runPrompt(
	ctx context.Context,
	promptCtx context.Context,
	cancel context.CancelFunc,
	request *assistant.PromptRequest,
	promptID uint64,
) {
	defer cancel()

	response, err := app.runtime.Prompt(promptCtx, request)
	if err != nil {
		app.postPromptError(ctx, promptID, err)

		return
	}

	app.postPromptDone(ctx, promptID, response)
}

type steeringReturnPayload struct {
	Text           string                `json:"text"`
	Images         []steeringReturnImage `json:"images,omitempty"`
	HideUserPrompt bool                  `json:"hide_user_prompt,omitempty"`
}

type steeringReturnImage struct {
	Name     string `json:"name,omitempty"`
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"data"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

func (app *App) steeringReturnHandler(
	ctx context.Context,
	promptID uint64,
) func([]assistant.SteeringMessage) {
	return func(messages []assistant.SteeringMessage) {
		payloads := make([]steeringReturnPayload, len(messages))
		for index, message := range messages {
			images := make([]steeringReturnImage, len(message.Images))
			for imageIndex, image := range message.Images {
				images[imageIndex] = steeringReturnImage{
					Name: image.Name, MIMEType: image.MIMEType, Data: image.Data,
					Width: image.Width, Height: image.Height,
				}
			}

			payloads[index] = steeringReturnPayload{
				Text: message.Text, Images: images, HideUserPrompt: message.HideUserPrompt,
			}
		}

		encoded, err := json.Marshal(payloads)
		if err != nil {
			app.postPromptError(ctx, promptID, err)

			return
		}

		app.postAsyncEvent(ctx, &asyncEvent{
			Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
			Kind: asyncEventSteeringReturn, Provider: "", Text: string(encoded), PromptID: promptID,
		})
	}
}

func (app *App) postPromptError(ctx context.Context, promptID uint64, err error) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventPromptError,
		Provider:      "",
		Text:          err.Error(),
		PromptID:      promptID,
	})
}

func (app *App) postPromptDone(ctx context.Context, promptID uint64, response *assistant.PromptResponse) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response:      response,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventPromptDone,
		Provider:      "",
		Text:          "",
		PromptID:      promptID,
	})
}
