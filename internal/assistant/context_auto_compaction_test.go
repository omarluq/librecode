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

func TestRuntime_AutoCompactsOversizedRequestBeforeProviderCall(t *testing.T) {
	t.Parallel()

	harness := newAutoCompactionRuntimeHarness(t, []string{"summary of old context", "final answer"}, 16_000)
	ctx := context.Background()
	session, err := harness.runtime.SessionRepository().CreateSession(ctx, testRuntimeCWD, "auto compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(
		t,
		harness.runtime.SessionRepository(),
		session.ID,
		nil,
		database.RoleUser,
		strings.Repeat("old ", 15_000),
	)
	appendRuntimeTestMessage(
		t,
		harness.runtime.SessionRepository(),
		session.ID,
		&old.ID,
		database.RoleAssistant,
		"tail",
	)
	request := newRuntimePromptRequest(testRuntimeCWD, "continue", "")
	request.SessionID = session.ID
	events := []assistant.StreamEvent{}
	request.OnEvent = func(event assistant.StreamEvent) {
		events = append(events, event)
	}

	response, err := harness.runtime.Prompt(ctx, request)

	require.NoError(t, err)
	assert.Equal(t, "final answer", response.Text)
	require.Len(t, harness.client.requests, 2)
	assert.True(t, harness.client.requests[0].DisableTools)
	assert.False(t, harness.client.requests[1].DisableTools)
	assert.Contains(t, harness.client.requests[1].Messages[0].Content, "summary of old context")
	assert.Equal(t, "continue", harness.client.requests[1].Messages[len(harness.client.requests[1].Messages)-1].Content)
	assert.Condition(t, func() bool {
		for _, event := range events {
			if isContextAutoCompactionEvent(&event) {
				return true
			}
		}
		return false
	})

	branch, err := harness.runtime.SessionRepository().Branch(ctx, session.ID, response.AssistantEntryID)
	require.NoError(t, err)
	roles := make([]database.EntryType, 0, len(branch))
	for index := range branch {
		roles = append(roles, branch[index].Type)
	}
	assert.Contains(t, roles, database.EntryTypeCompaction)
}

type autoCompactionRuntimeHarness struct {
	runtime *assistant.Runtime
	client  *recordingCompletionClient
}

func newAutoCompactionRuntimeHarness(
	t *testing.T,
	responses []string,
	contextWindow int,
) autoCompactionRuntimeHarness {
	t.Helper()

	client := newSequencedCompletionClient(responses...)
	runtime := newAutoCompactionTestRuntime(t, client, contextWindow)

	return autoCompactionRuntimeHarness{runtime: runtime, client: client}
}

func newAutoCompactionTestRuntime(
	t *testing.T,
	client assistant.CompletionClient,
	contextWindow int,
) *assistant.Runtime {
	t.Helper()

	runtime := newTestRuntimeWithContextWindow(t, client, contextWindow)
	runtimeConfig := testConfig()
	runtimeConfig.Context.KeepRecentTokens = 1
	runtimeConfig.Context.ProviderReserveTokens = 0
	runtimeConfig.Context.SafetyMarginTokens = 0
	// Reserve one token so post-response compaction tests keep a stable output headroom
	// and do not depend on off-by-one budget boundaries.
	runtimeConfig.Context.OutputReserveTokens = 1

	return assistant.NewRuntime(&assistant.RuntimeOptions{
		Config:     runtimeConfig,
		Sessions:   runtime.SessionRepository(),
		Extensions: nil,
		Cache:      assistant.NewResponseCache(false, 1, time.Minute),
		Events:     runtime.EventBus(),
		Models:     runtime.ModelRegistry(),
		Client:     client,
		Logger:     nil,
	})
}

func isContextAutoCompactionEvent(event *assistant.StreamEvent) bool {
	return event.Kind == assistant.StreamEventContextCompaction &&
		strings.Contains(event.Text, "context auto-compacted")
}
