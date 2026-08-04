package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestOpenAIResponseInputRoleMapping(t *testing.T) {
	t.Parallel()

	request := emptyCompletionRequest()
	setTestRequestMessages(request, mixedReplayMessages())

	input, err := openAIResponseInput(request.Request.Messages)

	require.NoError(t, err)
	assert.Len(t, input, 7)

	for _, item := range input {
		object, ok := item.(map[string]any)
		assert.True(t, ok)
		assert.JSONEq(t, jsonString(jsonUserRole), jsonString(object[jsonRoleKey]))
		assert.NotEmpty(t, object[jsonContentKey])
	}
}

func TestOpenAIResponseInputMultipartPayloadShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message llm.Message
		want    []map[string]any
	}{
		{
			name:    testImageWithText,
			message: testImageMessage(),
			want: []map[string]any{
				{jsonTypeKey: "input_text", jsonTextKey: testImagePrompt},
				{jsonTypeKey: "input_image", jsonImageURLKey: testImageDataURL},
			},
		},
		{
			name:    testImageOnly,
			message: testImageOnlyMessage(llm.RoleUser, testImageMIME, testImageData),
			want: []map[string]any{{
				jsonTypeKey: "input_image", jsonImageURLKey: testImageDataURL,
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input, err := openAIResponseInput([]llm.Message{
				test.message,
				llm.TextMessage(llm.RoleAssistant, testAssistantReplay),
				llm.TextMessage(llm.RoleTool, "tool output"),
			})

			require.NoError(t, err)
			require.Len(t, input, 2)
			user, ok := input[0].(map[string]any)
			require.True(t, ok)
			assert.JSONEq(t, jsonString(jsonUserRole), jsonString(user[jsonRoleKey]))
			assert.Equal(t, test.want, user[jsonContentKey])
			assert.Equal(t, map[string]any{
				jsonRoleKey: jsonUserRole, jsonContentKey: testAssistantReplay,
			}, input[1])
		})
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
