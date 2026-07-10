package terminal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/transcript"
)

func TestApplyStreamedSideEffectBlocks(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	blocks := []chatMessage{
		newChatMessage(transcript.RoleThinking, "thinking"),
		newChatMessage(transcript.RoleThinking, "\n\t"),
		newChatMessage(transcript.RoleToolResult, "tool result"),
		newChatMessage(transcript.RoleBashExecution, "bash result"),
		newChatMessage(transcript.RoleAssistant, "ignored assistant"),
		newChatMessage(transcript.RoleUser, "ignored user"),
		newChatMessage(transcript.RoleCustom, "ignored custom"),
		newChatMessage(transcript.RoleBranchSummary, "ignored branch"),
		newChatMessage(transcript.RoleCompactionSummary, "ignored compaction"),
	}

	thinkingBlocks, toolBlocks := app.applyStreamedSideEffectBlocks(blocks)

	assert.Equal(t, 1, thinkingBlocks)
	assert.Equal(t, 2, toolBlocks)

	assertPromptResponseRoles(t, app, []transcript.Role{
		transcript.RoleThinking,
		transcript.RoleToolResult,
		transcript.RoleBashExecution,
	})
}

func TestApplyPromptResponseNilClearsStreamedToolEvents(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = newTestActivePrompt(nil)
	app.streamedToolEvents = 2
	app.working = true
	app.appendStreamingBlock(transcript.RoleAssistant, "partial")

	app.applyPromptResponse(context.Background(), nil, app.activePrompt.ID)

	assert.Equal(t, 0, app.streamedToolEvents)
	assert.Nil(t, app.activePrompt)
	assert.False(t, app.working)
	assert.Empty(t, app.transcript.Streaming.Blocks)
}

func TestApplyRemainingSideEffectsSkipsStreamedBlocks(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	response := &assistant.PromptResponse{
		SessionID:        "",
		UserEntryID:      "",
		AssistantEntryID: "",
		Text:             "",
		Thinking:         []string{"streamed thinking", "remaining thinking"},
		ToolEvents: []assistant.ToolEvent{
			{Name: "read", ArgumentsJSON: "{}", DetailsJSON: "", Result: "streamed", Error: "", IsError: false},
			{Name: "write", ArgumentsJSON: "{}", DetailsJSON: "", Result: "remaining", Error: "", IsError: false},
		},
		Usage:  model.EmptyTokenUsage(),
		Cached: false,
	}

	app.applyRemainingSideEffects(response, 1, 1)

	assertPromptResponseRoles(t, app, []transcript.Role{
		transcript.RoleThinking,
		transcript.RoleToolResult,
	})

	assert.Equal(t, "remaining thinking", app.transcript.History[0].Content)
	assert.Contains(t, app.transcript.History[1].Content, "tool: write")
}

func assertPromptResponseRoles(t *testing.T, app *App, want []transcript.Role) {
	t.Helper()

	require.Len(t, app.transcript.History, len(want))

	for index, role := range want {
		assert.Equal(t, role, app.transcript.History[index].Role)
	}
}

func TestApplyPromptResponseAddsAssistantAndProcessesQueue(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("queued response"), nil)
	app := newPromptSendTestApp(t, client)
	app.activePrompt = newTestActivePrompt(nil)
	app.queuedMessages = []string{asyncTestQueuedText}

	app.applyPromptResponse(context.Background(), newTestPromptResponse("assistant response"), app.activePrompt.ID)

	waitForPromptRequest(t, client)
	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, "assistant response", app.transcript.History[0].Content)
	assert.True(t, app.working)
	assert.Empty(t, app.queuedMessages)
}

func TestApplyPromptResponseIgnoresStalePrompt(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = newTestActivePrompt(nil)
	app.working = true

	app.applyPromptResponse(context.Background(), newTestPromptResponse("stale response"), app.activePrompt.ID+1)

	assert.Empty(t, app.transcript.History)
	assert.True(t, app.working)
	assert.NotNil(t, app.activePrompt)
}

func TestApplyPromptResponsePreservesCanceledProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		response      *assistant.PromptResponse
		wantSessionID string
		wantUsage     model.TokenUsage
	}{
		{
			name:          "late response preserves bookkeeping",
			response:      newTestPromptResponseWithBookkeeping("late response"),
			wantSessionID: "response-session",
			wantUsage: model.TokenUsage{
				Breakdown:       nil,
				TopContributors: nil,
				ContextWindow:   100,
				ContextTokens:   25,
				InputTokens:     0,
				OutputTokens:    0,
			},
		},
		{
			name:          "nil response preserves progress",
			response:      nil,
			wantSessionID: "existing-session",
			wantUsage:     model.EmptyTokenUsage(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.sessionID = "existing-session"
			app.activePrompt = newTestActivePrompt(nil)
			app.activePrompt.Canceled = true
			app.working = true
			app.appendStreamingBlock(transcript.RoleAssistant, "partial")
			promptID := app.activePrompt.ID

			app.applyPromptResponse(context.Background(), test.response, promptID)
			require.NotEmpty(t, app.transcript.History)
			assert.Equal(t, "partial", app.transcript.History[0].Content)
			assert.NotContains(t, transcriptContents(app.transcript.History), "late response")
			assert.Equal(t, "response canceled; progress saved", app.statusMessage)
			assert.Equal(t, test.wantSessionID, app.sessionID)
			assert.Equal(t, test.wantUsage, app.tokenUsage)
			assert.False(t, app.working)
			assert.Empty(t, app.queuedMessages)
		})
	}
}

func transcriptContents(messages []chatMessage) []string {
	contents := make([]string, 0, len(messages))
	for _, message := range messages {
		contents = append(contents, message.Content)
	}

	return contents
}

func newTestPromptResponse(text string) *assistant.PromptResponse {
	return &assistant.PromptResponse{
		SessionID:        "",
		UserEntryID:      "",
		AssistantEntryID: "",
		Text:             text,
		Thinking:         nil,
		ToolEvents:       nil,
		Usage:            model.EmptyTokenUsage(),
		Cached:           false,
	}
}

func newTestPromptResponseWithBookkeeping(text string) *assistant.PromptResponse {
	response := newTestPromptResponse(text)
	response.SessionID = "response-session"
	response.Usage = model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   25,
		InputTokens:     10,
		OutputTokens:    5,
	}

	return response
}
