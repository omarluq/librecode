package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const (
	testUsagePrompt = "hello usage"
	testUsageReply  = "answer"
)

func TestBuildContextTracksLatestProviderUsageAnchor(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "usage", "")
	require.NoError(t, err)

	userEntry, err := repository.AppendMessage(ctx, session.ID, nil, newUsageTestMessage(
		database.RoleUser,
		testUsagePrompt,
		"",
		"",
	))
	require.NoError(t, err)

	modelFacing := true
	assistantUsage := &database.EntryTokenUsageEntity{
		ContextWindow: 1000,
		ContextTokens: 42,
		InputTokens:   40,
		OutputTokens:  2,
	}
	assistantEntry, err := repository.AppendMessageWithMetadata(ctx, session.ID, &userEntry.ID, newUsageTestMessage(
		database.RoleAssistant,
		testUsageReply,
		"openai",
		"gpt",
	), &modelFacing, assistantUsage)
	require.NoError(t, err)

	contextEntity, err := repository.BuildContext(ctx, session.ID, assistantEntry.ID)
	require.NoError(t, err)
	require.NotNil(t, contextEntity.UsageAnchor)
	assert.Equal(t, assistantEntry.ID, contextEntity.UsageAnchor.EntryID)
	assert.Equal(t, 1, contextEntity.UsageAnchor.MessageIndex)
	assert.Equal(t, 40, contextEntity.UsageAnchor.Usage.InputTokens)
}

func TestBuildContextClearsUsageAnchorAfterCompaction(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "usage", "")
	require.NoError(t, err)

	userEntry, err := repository.AppendMessage(ctx, session.ID, nil, newUsageTestMessage(
		database.RoleUser,
		testUsagePrompt,
		"",
		"",
	))
	require.NoError(t, err)

	modelFacing := true
	assistantEntry, err := repository.AppendMessageWithMetadata(ctx, session.ID, &userEntry.ID, newUsageTestMessage(
		database.RoleAssistant,
		testUsageReply,
		"",
		"",
	), &modelFacing, &database.EntryTokenUsageEntity{
		ContextWindow: 0,
		ContextTokens: 0,
		InputTokens:   40,
		OutputTokens:  0,
	})
	require.NoError(t, err)
	helper := sessionTestHelper{ctx, t, repository}
	compactionEntry := helper.appendCompactionSimple(
		session.ID, &assistantEntry.ID,
		"summary", userEntry.ID, 100,
	)

	contextEntity, err := repository.BuildContext(ctx, session.ID, compactionEntry.ID)
	require.NoError(t, err)
	assert.Nil(t, contextEntity.UsageAnchor)
}

func newUsageTestMessage(role database.Role, content, provider, model string) *database.MessageEntity {
	return &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      role,
		Content:   content,
		Provider:  provider,
		Model:     model,
	}
}
