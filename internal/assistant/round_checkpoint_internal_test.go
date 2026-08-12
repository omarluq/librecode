package assistant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
)

const (
	testRoundStart    = "start"
	testRoundSteering = "change direction"
	testRoundRun      = "run"
)

func TestRuntimeRoundCheckpointPersistsBeforeReturningSteering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newRoundPersistenceTestHarness(t, "checkpoint")
	lineage := newPromptLineage(harness.rootID)
	require.NoError(t, harness.runtime.steering.register(harness.sessionID, harness.rootID))
	require.NoError(t, harness.runtime.Steer(ctx, &SteeringRequest{
		SessionID: harness.sessionID, RunID: harness.rootID,
		Text: testRoundSteering, Images: nil, HideUserPrompt: false,
	}))

	messages, err := harness.runtime.roundCheckpoint(harness.sessionID, lineage)(ctx, &llm.CompletedRound{
		Assistant:   llm.Message{Metadata: nil, Role: llm.RoleAssistant, Content: []llm.Part{llm.TextPart("working")}},
		ToolResults: nil, FinishReason: llm.FinishReasonStop, Usage: llm.EmptyUsage(),
	})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, llm.RoleUser, messages[0].Role)
	assert.Equal(t, testRoundSteering, messages[0].Content[0].Text)
	assert.True(t, lineage.checkpointed)

	persisted, err := harness.repository.Messages(ctx, harness.sessionID)
	require.NoError(t, err)
	require.Len(t, persisted, 3)
	assert.Equal(t, []database.Role{
		database.RoleUser, database.RoleAssistant, database.RoleUser,
	}, []database.Role{persisted[0].Role, persisted[1].Role, persisted[2].Role})
	assert.Equal(t, "working", persisted[1].Content)
	assert.Equal(t, testRoundSteering, persisted[2].Content)
}

func TestRuntimeRoundCheckpointPersistsPendingRoundPrefixBeforeSteering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newRoundPersistenceTestHarness(t, "checkpoint prefix")
	lineage := newPromptLineage(harness.rootID)
	require.NoError(t, harness.runtime.steering.register(harness.sessionID, harness.rootID))
	checkpoint := harness.runtime.roundCheckpoint(harness.sessionID, lineage)

	messages, err := checkpoint(ctx, &llm.CompletedRound{
		Assistant: llm.Message{Metadata: nil, Role: llm.RoleAssistant, Content: []llm.Part{
			llm.TextPart("first round"),
		}},
		ToolResults: nil, FinishReason: llm.FinishReasonToolCalls, Usage: llm.EmptyUsage(),
	})
	require.NoError(t, err)
	assert.Nil(t, messages)

	require.NoError(t, harness.runtime.Steer(ctx, &SteeringRequest{
		SessionID: harness.sessionID, RunID: harness.rootID,
		Text: testRoundSteering, Images: nil, HideUserPrompt: false,
	}))
	messages, err = checkpoint(ctx, &llm.CompletedRound{
		Assistant: llm.Message{Metadata: nil, Role: llm.RoleAssistant, Content: []llm.Part{
			llm.TextPart("second round"),
		}},
		ToolResults: nil, FinishReason: llm.FinishReasonToolCalls, Usage: llm.EmptyUsage(),
	})
	require.NoError(t, err)
	require.Len(t, messages, 1)

	persisted, err := harness.repository.Messages(ctx, harness.sessionID)
	require.NoError(t, err)
	require.Len(t, persisted, 4)
	assert.Equal(t, []string{testRoundStart, "first round", "second round", testRoundSteering}, []string{
		persisted[0].Content, persisted[1].Content, persisted[2].Content, persisted[3].Content,
	})
}

func TestRuntimeRoundCheckpointToolRoundWithoutSteeringKeepsInboxActive(t *testing.T) {
	t.Parallel()

	runtime := NewRuntimeForTest(nil)
	lineage := newPromptLineage(testRoundRun)

	require.NoError(t, runtime.steering.register("session", testRoundRun))

	messages, err := runtime.roundCheckpoint("session", lineage)(context.Background(), &llm.CompletedRound{
		Assistant:   llm.Message{Metadata: nil, Role: llm.RoleAssistant, Content: nil},
		ToolResults: nil, FinishReason: llm.FinishReasonToolCalls, Usage: llm.EmptyUsage(),
	})
	require.NoError(t, err)
	assert.Nil(t, messages)
	require.NoError(t, runtime.Steer(context.Background(), &SteeringRequest{
		SessionID: testSteeringSession, RunID: testRoundRun, Text: "next boundary", Images: nil, HideUserPrompt: false,
	}))
}

func TestRuntimeRoundCheckpointFinalRoundWithoutSteeringSettlesInbox(t *testing.T) {
	t.Parallel()

	runtime := NewRuntimeForTest(nil)
	lineage := newPromptLineage(testRoundRun)

	require.NoError(t, runtime.steering.register("session", testRoundRun))

	messages, err := runtime.roundCheckpoint("session", lineage)(context.Background(), &llm.CompletedRound{
		Assistant:   llm.Message{Metadata: nil, Role: llm.RoleAssistant, Content: nil},
		ToolResults: nil, FinishReason: llm.FinishReasonStop, Usage: llm.EmptyUsage(),
	})
	require.NoError(t, err)
	assert.Nil(t, messages)
	assert.False(t, lineage.checkpointed)
	assert.ErrorIs(t, runtime.Steer(context.Background(), &SteeringRequest{
		SessionID: testSteeringSession, RunID: testRoundRun, Text: "too late", Images: nil, HideUserPrompt: false,
	}), ErrSteeringInactive)
}
