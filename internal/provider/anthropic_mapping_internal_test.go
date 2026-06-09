package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func TestAnthropicThinkingEffortMappings(t *testing.T) {
	t.Parallel()

	mappedHigh := thinkingHigh
	request := testCompletionRequestAuth("sk-ant-api03-secret")
	request.Model.ThinkingLevelMap = map[model.ThinkingLevel]*string{model.ThinkingLevel(thinkingLow): &mappedHigh}
	request.ThinkingLevel = thinkingLow
	assert.Equal(t, thinkingHigh, anthropicThinkingEffort(request))

	request.Model.ThinkingLevelMap = nil
	for _, testCase := range []struct {
		level string
		want  string
	}{
		{level: thinkingMinimal, want: thinkingLow},
		{level: thinkingMedium, want: thinkingMedium},
		{level: thinkingHigh, want: thinkingHigh},
		{level: thinkingXHigh, want: thinkingXHigh},
		{level: "unknown", want: thinkingHigh},
	} {
		t.Run(testCase.level, func(t *testing.T) {
			t.Parallel()

			request := testCompletionRequestAuth("sk-ant-api03-secret")
			request.ThinkingLevel = testCase.level
			assert.Equal(t, testCase.want, anthropicThinkingEffort(request))
		})
	}
}

func TestAnthropicThinkingBudgetMappings(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1024, anthropicThinkingBudget(thinkingMinimal))
	assert.Equal(t, 4096, anthropicThinkingBudget(thinkingLow))
	assert.Equal(t, 20480, anthropicThinkingBudget(thinkingHigh))
	assert.Equal(t, 20480, anthropicThinkingBudget(thinkingXHigh))
	assert.Equal(t, 10240, anthropicThinkingBudget(thinkingMedium))
}

func TestAnthropicBetaFeatures(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	request.Model.Reasoning = false
	assert.Empty(t, anthropicBetaFeatures(request))

	request.Model.Reasoning = true
	request.Model.ID = "claude-sonnet-4-5"
	assert.Contains(t, anthropicBetaFeatures(request), "interleaved-thinking-2025-05-14")

	request.Model.ID = testAdaptiveAnthropicModelID
	assert.Empty(t, anthropicBetaFeatures(request))
}

func TestAnthropicToolArgumentsHandlesMalformedAndScalarInput(t *testing.T) {
	t.Parallel()

	arguments, argumentsJSON := anthropicToolArguments(func() {})
	assert.Equal(t, map[string]any{}, arguments)
	assert.Equal(t, "{}", argumentsJSON)

	arguments, argumentsJSON = anthropicToolArguments("plain")
	assert.Equal(t, map[string]any{}, arguments)
	assert.JSONEq(t, `"plain"`, argumentsJSON)
}

func TestAnthropicAssistantToolMessageMapsProviderNames(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("anthropic-claude", "subscription-access-token")
	message := anthropicAssistantToolMessage(request, []ToolCall{{
		Arguments:     map[string]any{jsonPathKey: testToolPath},
		ID:            testAnthropicToolUseID,
		Name:          jsonReadToolName,
		ArgumentsJSON: testToolArgumentsJSON,
		TextFallback:  false,
	}})

	blocks, ok := message[jsonContentKey].([]map[string]any)
	require.True(t, ok)
	require.Len(t, blocks, 1)
	assert.Equal(t, anthropicToolUseType, blocks[0][jsonTypeKey])
	assert.Equal(t, anthropicReadToolName, blocks[0][jsonToolNameKey])
}

func TestAnthropicMessagesAndRoles(t *testing.T) {
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

	converted := anthropicMessages(messages)

	assert.Len(t, converted, 6)
	assert.Equal(t, jsonUserRole, converted[0][jsonRoleKey])
	assert.Equal(t, jsonAssistantRole, converted[1][jsonRoleKey])
	for _, role := range []database.Role{database.RoleToolResult, database.RoleThinking} {
		mapped, ok := anthropicRole(role)
		assert.False(t, ok)
		assert.Empty(t, mapped)
	}
}

func TestAnthropicLocalToolNameFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "write", input: anthropicWriteToolName, want: jsonWriteToolName},
		{name: "edit", input: anthropicEditToolName, want: jsonEditToolName},
		{name: "bash", input: anthropicBashToolName, want: jsonBashToolName},
		{name: "grep", input: anthropicGrepToolName, want: jsonGrepToolName},
		{name: "find", input: anthropicFindToolName, want: jsonFindToolName},
		{name: "ls", input: anthropicLSToolName, want: jsonLSToolName},
		{name: "list alias", input: "List", want: jsonLSToolName},
		{name: "unknown", input: "Unknown", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, anthropicLocalToolName(test.input))
		})
	}
}
