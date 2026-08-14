package compaction

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestGroupsFromMessagesKeepsToolResultsWithTurn(t *testing.T) {
	t.Parallel()

	messages := []database.MessageEntity{
		testGroupMessage(database.RoleUser, "one"),
		testGroupMessage(database.RoleAssistant, "call"),
		testGroupMessage(database.RoleToolResult, "ok"),
		testGroupMessage(database.RoleToolResult, "failed"),
		testGroupMessage(database.RoleUser, "two"),
		testGroupMessage(database.RoleAssistant, "done"),
	}
	groups := GroupsFromMessages(messages, []string{"1", "2", "3", "4", "5", "6"}, func(string) int { return 1 })

	require.Len(t, groups, 2)
	assert.Len(t, groups[0].Messages, 4)
	assert.Equal(t, database.RoleToolResult, groups[0].Messages[3].Role)
	assert.Len(t, groups[1].Messages, 2)
}

func TestPartitionRejectsInvalidGroups(t *testing.T) {
	t.Parallel()

	_, err := Partition([]SemanticGroup{{
		Kind: SemanticGroupHistoryTurn, EntryIDs: nil, Messages: nil, Tokens: 0,
	}}, 100, MaxChunksPerReductionRound)
	require.Error(t, err)

	orphan := testGroupMessage(database.RoleToolResult, "orphan")
	_, err = Partition([]SemanticGroup{{
		Kind: SemanticGroupHistoryTurn, EntryIDs: nil,
		Messages: []database.MessageEntity{orphan}, Tokens: 1,
	}}, 100, MaxChunksPerReductionRound)
	require.Error(t, err)
}

func TestPartition(t *testing.T) {
	t.Parallel()

	groups := []SemanticGroup{
		newTestSemanticGroup("a", 3),
		newTestSemanticGroup("b", 4),
		newTestSemanticGroup("c", 5),
	}
	chunks, err := Partition(groups, 7, MaxChunksPerReductionRound)

	require.NoError(t, err)
	require.Len(t, chunks, 2)
	assert.Equal(t, 7, chunks[0].Tokens)
	assert.Equal(t, 5, chunks[1].Tokens)

	_, err = Partition(groups, 2, MaxChunksPerReductionRound)
	require.ErrorIs(t, err, ErrSummaryIndivisibleGroup)
}

func newTestSemanticGroup(text string, tokens int) SemanticGroup {
	return SemanticGroup{
		Kind:     "",
		EntryIDs: nil,
		Messages: []database.MessageEntity{testGroupMessage(database.RoleUser, text)},
		Tokens:   tokens,
	}
}

func testGroupMessage(role database.Role, content string) database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Time{},
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
		Parts:     nil,
	}
}
