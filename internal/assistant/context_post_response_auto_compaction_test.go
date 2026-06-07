package assistant_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

func TestRuntime_AutoCompactsAfterResponseNearThreshold(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	harness := newAutoCompactionRuntimeHarness(t, []string{"final answer", "summary of completed work"}, 16_000)
	ctx := context.Background()
	session, err := harness.runtime.SessionRepository().CreateSession(ctx, testRuntimeCWD, "post compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(
		t,
		harness.runtime.SessionRepository(),
		session.ID,
		nil,
		database.RoleUser,
		strings.Repeat("old ", 12_000),
	)
	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID
	request.ParentEntryID = &old.ID
	events := []assistant.StreamEvent{}
	request.OnEvent = func(event assistant.StreamEvent) {
		events = append(events, event)
	}

	response, err := harness.runtime.Prompt(ctx, request)

	require.NoError(t, err)
	assert.Equal(t, "final answer", response.Text)
	assert.Greater(t, response.Usage.ContextTokens, 0)
	require.Len(t, harness.client.requests, 2)
	assert.False(t, harness.client.requests[0].DisableTools)
	assert.True(t, harness.client.requests[1].DisableTools)
	assert.Contains(t, harness.client.requests[1].Messages[0].Content, "old")
	assertPostResponseCompactionEvent(t, events)
	assertPostResponseUsageSnapshot(t, events)

	leaf, found, err := harness.runtime.SessionRepository().LeafEntry(ctx, session.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.EntryTypeCompaction, leaf.Type)
	assert.Equal(t, response.AssistantEntryID, *leaf.ParentID)

	branch, err := harness.runtime.SessionRepository().Branch(ctx, session.ID, leaf.ID)
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

func assertPostResponseUsageSnapshot(t *testing.T, events []assistant.StreamEvent) {
	t.Helper()

	for _, event := range events {
		if event.Kind != assistant.StreamEventUsageSnapshot || event.Usage == nil {
			continue
		}
		if event.Usage.ContextTokens > 0 {
			return
		}
	}
	assert.Fail(t, "expected post-response auto-compaction usage snapshot")
}
