package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/database"
)

func TestOpenAIResponseInputRoleMapping(t *testing.T) {
	t.Parallel()

	messages := []database.MessageEntity{
		providerTestMessageEntity(database.RoleUser, jsonUserRole),
		providerTestMessageEntity(database.RoleAssistant, jsonAssistantRole),
		providerTestMessageEntity(database.RoleBranchSummary, testBranchContent),
		providerTestMessageEntity(database.RoleCompactionSummary, testCompactionContent),
		providerTestMessageEntity(database.RoleCustom, testCustomContent),
		providerTestMessageEntity(database.RoleBashExecution, jsonBashToolName),
		providerTestMessageEntity(database.RoleToolResult, jsonToolRole),
		providerTestMessageEntity(database.RoleThinking, testThinkingDelta),
		providerTestMessageEntity(database.RoleUser, ""),
	}

	input := openAIResponseInput(messages)

	assert.Len(t, input, 6)
	for _, item := range input {
		object, ok := item.(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, jsonUserRole, object[jsonRoleKey])
		assert.NotEmpty(t, object[jsonContentKey])
	}
}

func TestOpenAIResponseInputRoleRejectsNonReplayRoles(t *testing.T) {
	t.Parallel()

	for _, role := range []database.Role{database.RoleToolResult, database.RoleThinking} {
		mapped, ok := openAIResponseInputRole(role)

		assert.False(t, ok)
		assert.Empty(t, mapped)
	}
}

func TestCompactResponseMessagesMergesConsecutiveAssistantMessages(t *testing.T) {
	t.Parallel()

	messages := []database.MessageEntity{
		providerTestMessageEntity(database.RoleUser, jsonUserRole),
		providerTestMessageEntity(database.RoleAssistant, "first"),
		providerTestMessageEntity(database.RoleAssistant, "  "),
		providerTestMessageEntity(database.RoleAssistant, "second"),
		providerTestMessageEntity(database.RoleUser, "next"),
		providerTestMessageEntity(database.RoleAssistant, "tail"),
	}

	compacted := compactResponseMessages(messages)

	assert.Len(t, compacted, 4)
	assert.Equal(t, database.RoleUser, compacted[0].Role)
	assert.Equal(t, database.RoleAssistant, compacted[1].Role)
	assert.Equal(t, "first\n\nsecond", compacted[1].Content)
	assert.Equal(t, database.RoleUser, compacted[2].Role)
	assert.Equal(t, database.RoleAssistant, compacted[3].Role)
	assert.Equal(t, "tail", compacted[3].Content)
}

func TestCompactResponseMessagesDropsBlankAssistantRuns(t *testing.T) {
	t.Parallel()

	messages := []database.MessageEntity{
		providerTestMessageEntity(database.RoleAssistant, "  "),
		providerTestMessageEntity(database.RoleUser, jsonUserRole),
	}

	compacted := compactResponseMessages(messages)

	assert.Equal(t, []database.MessageEntity{providerTestMessageEntity(database.RoleUser, jsonUserRole)}, compacted)
}
