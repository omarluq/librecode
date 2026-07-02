package terminal

import (
	"context"
	"slices"
	"strings"
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

	if got, want := thinkingBlocks, 1; got != want {
		t.Fatalf("thinkingBlocks = %d, want %d", got, want)
	}

	if got, want := toolBlocks, 2; got != want {
		t.Fatalf("toolBlocks = %d, want %d", got, want)
	}

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

	if got, want := app.transcript.History[0].Content, "remaining thinking"; got != want {
		t.Fatalf("thinking content = %q, want %q", got, want)
	}

	if got := app.transcript.History[1].Content; !strings.Contains(got, "tool: write") {
		t.Fatalf("tool content = %q, want write tool", got)
	}
}

func assertPromptResponseRoles(t *testing.T, app *App, want []transcript.Role) {
	t.Helper()

	if got := len(app.transcript.History); got != len(want) {
		t.Fatalf("message count = %d, want %d", got, len(want))
	}

	for index, role := range want {
		if got := app.transcript.History[index].Role; got != role {
			t.Fatalf("message[%d].Role = %q, want %q", index, got, role)
		}
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
	assert.True(t, slices.Equal(app.queuedMessages, []string(nil)))
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

	client := newTerminalPromptClient(newTerminalCompletionResult("queued response"), nil)
	app := newPromptSendTestApp(t, client)
	app.activePrompt = newTestActivePrompt(nil)
	app.activePrompt.Canceled = true
	app.working = true
	app.queuedMessages = []string{asyncTestQueuedText}
	app.appendStreamingBlock(transcript.RoleAssistant, "partial")

	app.applyPromptResponse(context.Background(), newTestPromptResponse("late response"), app.activePrompt.ID)

	waitForPromptRequest(t, client)
	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, "partial", app.transcript.History[0].Content)
	assert.NotContains(t, transcriptContents(app.transcript.History), "late response")
	assert.Equal(t, "response canceled; progress saved", app.statusMessage)
	assert.True(t, app.working)
	assert.True(t, slices.Equal(app.queuedMessages, []string(nil)))
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
