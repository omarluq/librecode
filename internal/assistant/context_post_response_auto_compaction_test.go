package assistant_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

func TestRuntime_AutoCompactsAfterResponseNearThreshold(t *testing.T) {
	t.Parallel()

	client := &sequencedCompletionClient{
		responses: []string{"final answer", "summary of completed work"},
		requests:  nil,
	}
	runtime := newTestRuntimeWithContextWindow(t, client, 16_000)
	runtimeConfig := testConfig()
	runtimeConfig.Context.KeepRecentTokens = 1
	runtimeConfig.Context.ProviderReserveTokens = 0
	runtimeConfig.Context.SafetyMarginTokens = 0
	runtimeConfig.Context.OutputReserveTokens = 0
	runtime = assistant.NewRuntime(
		runtimeConfig,
		runtime.SessionRepository(),
		nil,
		assistant.NewResponseCache(false, 1, time.Minute),
		runtime.EventBus(),
		runtime.ModelRegistry(),
		client,
		nil,
	)

	ctx := context.Background()
	session, err := runtime.SessionRepository().CreateSession(ctx, testRuntimeCWD, "post compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(
		t,
		runtime.SessionRepository(),
		session.ID,
		nil,
		database.RoleUser,
		strings.Repeat("old ", 1_000),
	)
	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID
	request.ParentEntryID = &old.ID
	events := []assistant.StreamEvent{}
	request.OnEvent = func(event assistant.StreamEvent) {
		events = append(events, event)
	}

	response, err := runtime.Prompt(ctx, request)

	require.NoError(t, err)
	assert.Equal(t, "final answer", response.Text)
	require.Len(t, client.requests, 2)
	assert.False(t, client.requests[0].DisableTools)
	assert.True(t, client.requests[1].DisableTools)
	assert.Contains(t, client.requests[1].Messages[0].Content, "old")
	assertPostResponseCompactionEvent(t, events)

	leaf, found, err := runtime.SessionRepository().LeafEntry(ctx, session.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.EntryTypeCompaction, leaf.Type)
	assert.Equal(t, response.AssistantEntryID, *leaf.ParentID)

	branch, err := runtime.SessionRepository().Branch(ctx, session.ID, leaf.ID)
	require.NoError(t, err)
	assert.Equal(t, database.EntryTypeCompaction, branch[len(branch)-1].Type)
}

func TestShouldAutoCompactAfterResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		inputTokens   int
		usableInput   int
		contextWindow int
		want          bool
	}{
		{name: "below threshold", inputTokens: 79, usableInput: 100, contextWindow: 200, want: false},
		{name: "at threshold", inputTokens: 80, usableInput: 100, contextWindow: 200, want: true},
		{name: "above threshold", inputTokens: 99, usableInput: 100, contextWindow: 200, want: true},
		{name: "unknown context window", inputTokens: 80, usableInput: 100, contextWindow: 0, want: false},
		{name: "unknown usable input", inputTokens: 80, usableInput: 0, contextWindow: 200, want: false},
		{name: "empty context", inputTokens: 0, usableInput: 100, contextWindow: 200, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := assistant.ShouldAutoCompactAfterResponseForTest(
				testCase.inputTokens,
				testCase.usableInput,
				testCase.contextWindow,
			)

			assert.Equal(t, testCase.want, got)
		})
	}
}

func assertPostResponseCompactionEvent(t *testing.T, events []assistant.StreamEvent) {
	t.Helper()

	foundStart := false
	foundDone := false
	for _, event := range events {
		if event.Kind != assistant.StreamEventContextCompaction {
			continue
		}
		foundStart = foundStart || strings.Contains(event.Text, "context auto-compacting after response")
		foundDone = foundDone || strings.Contains(event.Text, "context auto-compacted after response")
	}
	assert.True(t, foundStart, "expected post-response auto-compaction start event")
	assert.True(t, foundDone, "expected post-response auto-compaction completion event")
}
