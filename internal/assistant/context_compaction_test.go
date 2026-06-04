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
	"github.com/omarluq/librecode/internal/model"
)

func TestRuntime_CompactSessionSummarizesOldHistoryAndKeepsTail(t *testing.T) {
	t.Parallel()

	client := &compactionCompletionClient{summary: "summary of compacted work", requests: nil}
	runtime, repository := newTestRuntimeWithClient(t, client)
	runtimeConfig := testConfig()
	runtimeConfig.Context.KeepRecentTokens = 10
	runtime = assistant.NewRuntime(
		runtimeConfig,
		repository,
		nil,
		assistant.NewResponseCache(false, 1, time.Minute),
		runtime.EventBus(),
		runtime.ModelRegistry(),
		client,
		nil,
	)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	first := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, "old user goal")
	second := appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&first.ID,
		database.RoleAssistant,
		"old assistant answer",
	)
	third := appendRuntimeTestMessage(t, repository, session.ID, &second.ID, database.RoleUser, "recent user tail")
	fourth := appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&third.ID,
		database.RoleAssistant,
		"recent assistant tail",
	)

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, database.EntryTypeCompaction, entry.Type)
	assert.Equal(t, "summary of compacted work", entry.Summary)
	assert.Equal(t, third.ID, entry.CompactionFirstKeptEntryID)
	assert.Positive(t, entry.CompactionTokensBefore)
	require.NotNil(t, entry.ParentID)
	assert.Equal(t, fourth.ID, *entry.ParentID)
	require.Len(t, client.requests, 1)
	assert.True(t, client.requests[0].DisableTools)
	assert.Contains(t, client.requests[0].SystemPrompt, "Summarize the conversation history")
	require.Len(t, client.requests[0].Messages, 2)
	assert.Equal(t, "old user goal", client.requests[0].Messages[0].Content)
	assert.Equal(t, "old assistant answer", client.requests[0].Messages[1].Content)

	contextEntity, err := repository.BuildContext(ctx, session.ID, entry.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 3)
	assert.Equal(t, database.RoleCompactionSummary, contextEntity.Messages[0].Role)
	assert.Equal(t, "summary of compacted work", contextEntity.Messages[0].Content)
	assert.Equal(t, "recent user tail", contextEntity.Messages[1].Content)
	assert.Equal(t, "recent assistant tail", contextEntity.Messages[2].Content)
}

func TestRuntime_CompactSessionChainsNextPromptFromCompactionEntry(t *testing.T) {
	t.Parallel()

	client := &compactionCompletionClient{summary: "summary of compacted work", requests: nil}
	runtime, repository := newTestRuntimeWithClient(t, client)
	runtimeConfig := testConfig()
	runtimeConfig.Context.KeepRecentTokens = 1
	runtime = assistant.NewRuntime(
		runtimeConfig,
		repository,
		nil,
		assistant.NewResponseCache(false, 1, time.Minute),
		runtime.EventBus(),
		runtime.ModelRegistry(),
		client,
		nil,
	)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 1000))
	appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "tail")

	compactionEntry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)
	require.NoError(t, err)
	followUp := appendRuntimeTestMessage(t, repository, session.ID, &compactionEntry.ID, database.RoleUser, "continue")

	contextEntity, err := repository.BuildContext(ctx, session.ID, followUp.ID)

	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 3)
	assert.Equal(t, database.RoleCompactionSummary, contextEntity.Messages[0].Role)
	assert.Equal(t, "tail", contextEntity.Messages[1].Content)
	assert.Equal(t, "continue", contextEntity.Messages[2].Content)
}

func TestRuntime_CompactSessionRejectsShortHistory(t *testing.T) {
	t.Parallel()

	client := &compactionCompletionClient{summary: "unused", requests: nil}
	runtime, repository := newTestRuntimeWithClient(t, client)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, "only one message")

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enough old history to compact")
	assert.Nil(t, entry)
	assert.Empty(t, client.requests)
}

func TestRuntime_CompactSessionFromUsesExplicitParent(t *testing.T) {
	t.Parallel()

	client := &compactionCompletionClient{summary: "summary of selected branch", requests: nil}
	runtime, repository := newTestRuntimeWithClient(t, client)
	runtimeConfig := testConfig()
	runtimeConfig.Context.KeepRecentTokens = 1
	runtime = assistant.NewRuntime(
		runtimeConfig,
		repository,
		nil,
		assistant.NewResponseCache(false, 1, time.Minute),
		runtime.EventBus(),
		runtime.ModelRegistry(),
		client,
		nil,
	)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	root := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("root ", 1000))
	selectedTail := appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&root.ID,
		database.RoleAssistant,
		"selected tail",
	)
	_ = appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&root.ID,
		database.RoleAssistant,
		"other branch tail",
	)

	entry, err := runtime.CompactSessionFrom(ctx, session.ID, testRuntimeCWD, &selectedTail.ID)

	require.NoError(t, err)
	require.NotNil(t, entry.ParentID)
	assert.Equal(t, selectedTail.ID, *entry.ParentID)
	assert.Equal(t, selectedTail.ID, entry.CompactionFirstKeptEntryID)
	require.Len(t, client.requests, 1)
	require.Len(t, client.requests[0].Messages, 1)
	assert.Equal(t, root.Message.Content, client.requests[0].Messages[0].Content)
}

func TestRuntime_CompactSessionRejectsEmptySummary(t *testing.T) {
	t.Parallel()

	client := &compactionCompletionClient{summary: "   ", requests: nil}
	runtime, repository := newTestRuntimeWithClient(t, client)
	runtimeConfig := testConfig()
	runtimeConfig.Context.KeepRecentTokens = 1
	runtime = assistant.NewRuntime(
		runtimeConfig,
		repository,
		nil,
		assistant.NewResponseCache(false, 1, time.Minute),
		runtime.EventBus(),
		runtime.ModelRegistry(),
		client,
		nil,
	)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 1000))
	appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "tail")

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "compaction summary was empty")
	assert.Nil(t, entry)
}

func appendRuntimeTestMessage(
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	parentID *string,
	role database.Role,
	content string,
) *database.EntryEntity {
	t.Helper()

	entry, err := repository.AppendMessage(context.Background(), sessionID, parentID, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	})
	require.NoError(t, err)

	return entry
}

type compactionCompletionClient struct {
	summary  string
	requests []*assistant.CompletionRequest
}

func (client *compactionCompletionClient) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.requests = append(client.requests, request)
	text := client.summary
	if text == "" {
		text = "summary: " + strings.TrimSpace(request.Messages[0].Content)
	}

	return &assistant.CompletionResult{
		Text:       text,
		Thinking:   nil,
		ToolEvents: nil,
		Usage:      model.EmptyTokenUsage(),
	}, nil
}
