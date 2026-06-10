package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/llm"
)

func TestOpenAIResponseInputRoleMapping(t *testing.T) {
	t.Parallel()

	request := emptyCompletionRequest()
	setTestRequestMessages(request, mixedReplayMessages())

	input := openAIResponseInput(request.Request.Messages)

	assert.Len(t, input, 7)
	for _, item := range input {
		object, ok := item.(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, jsonUserRole, object[jsonRoleKey])
		assert.NotEmpty(t, object[jsonContentKey])
	}
}

func TestOpenAIResponseInputRoleRejectsNonReplayRoles(t *testing.T) {
	t.Parallel()

	mapped, ok := openAIResponseInputRole(llm.RoleTool)

	assert.False(t, ok)
	assert.Empty(t, mapped)
}

func TestCompactResponseMessagesMergesConsecutiveAssistantMessages(t *testing.T) {
	t.Parallel()

	messages := []llm.Message{
		llm.TextMessage(llm.RoleUser, jsonUserRole),
		llm.TextMessage(llm.RoleAssistant, "first"),
		llm.TextMessage(llm.RoleAssistant, "  "),
		llm.TextMessage(llm.RoleAssistant, "second"),
		llm.TextMessage(llm.RoleUser, "next"),
		llm.TextMessage(llm.RoleAssistant, "tail"),
	}

	compacted := compactResponseMessages(messages)

	assert.Len(t, compacted, 4)
	assert.Equal(t, llm.RoleUser, compacted[0].Role)
	assert.Equal(t, llm.RoleAssistant, compacted[1].Role)
	assert.Equal(t, "first\n\nsecond", messageText(compacted[1]))
	assert.Equal(t, llm.RoleUser, compacted[2].Role)
	assert.Equal(t, llm.RoleAssistant, compacted[3].Role)
	assert.Equal(t, "tail", messageText(compacted[3]))
}

func TestCompactResponseMessagesDropsBlankAssistantRuns(t *testing.T) {
	t.Parallel()

	messages := []llm.Message{
		llm.TextMessage(llm.RoleAssistant, "  "),
		llm.TextMessage(llm.RoleUser, jsonUserRole),
	}

	compacted := compactResponseMessages(messages)

	assert.Equal(t, []llm.Message{llm.TextMessage(llm.RoleUser, jsonUserRole)}, compacted)
}
