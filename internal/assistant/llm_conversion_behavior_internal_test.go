package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/llmconv"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	testLLMMutatedLabel      = "changed"
	testLLMMutatedAgainLabel = "mutated"
)

func TestLLMMessageFromDatabaseRejectsNilBlankAndUnknown(t *testing.T) {
	t.Parallel()

	message, converted := llmMessageFromDatabase(nil)
	assert.False(t, converted)
	assert.Empty(t, message.Role)

	blankMessage := testMessageEntity(database.RoleUser, "   ")
	message, converted = llmMessageFromDatabase(&blankMessage)
	assert.False(t, converted)
	assert.Empty(t, message.Role)
}

func TestLLMRoleFromDatabaseMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role database.Role
		want llm.Role
		ok   bool
	}{
		{role: database.RoleUser, want: llm.RoleUser, ok: true},
		{role: database.RoleBranchSummary, want: llm.RoleUser, ok: true},
		{role: database.RoleCompactionSummary, want: llm.RoleUser, ok: true},
		{role: database.RoleCustom, want: llm.RoleUser, ok: true},
		{role: database.RoleBashExecution, want: llm.RoleUser, ok: true},
		{role: database.RoleAssistant, want: llm.RoleAssistant, ok: true},
		{role: database.RoleThinking, want: llm.RoleAssistant, ok: true},
		{role: database.RoleToolResult, want: llm.RoleTool, ok: true},
	}
	for _, testCase := range tests {
		t.Run(string(testCase.role), func(t *testing.T) {
			t.Parallel()

			got, ok := llmRoleFromDatabase(testCase.role)
			assert.Equal(t, testCase.ok, ok)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestLLMResponseAndToolResultNilPaths(t *testing.T) {
	t.Parallel()

	response := llmResponseFromCompletionResult(nil)
	assert.Equal(t, llm.FinishReasonUnknown, response.FinishReason)
	assert.False(t, response.Usage.HasAny())

	result := llmToolResultFromEvent(nil)
	require.NotNil(t, result)
	assert.Empty(t, result.Name)
	assert.Empty(t, result.Content)
}

func TestLLMModelAndToolDefinitionNilPaths(t *testing.T) {
	t.Parallel()

	modelRef := llmModelRefFromModel(nil)
	assert.Empty(t, modelRef.ID)

	definition := llmToolDefinitionFromTool(nil)
	assert.Empty(t, definition.Name)
	assert.Nil(t, definition.Schema)
}

func TestLLMTokenContributorConversionsCloneAndRoundTrip(t *testing.T) {
	t.Parallel()

	contributors := []model.TokenContributor{
		{Label: contextwindow.BreakdownHistory, Role: string(llm.RoleUser), Preview: "hello", Tokens: 4, Chars: 20},
	}
	converted := llmconv.TokenContributorsFromModel(contributors)
	require.Len(t, converted, 1)
	assert.Equal(t, contextwindow.BreakdownHistory, converted[0].Label)

	contributors[0].Label = testLLMMutatedLabel
	assert.Equal(t, contextwindow.BreakdownHistory, converted[0].Label)

	roundTrip := llmconv.TokenContributorsToModel(converted)
	require.Len(t, roundTrip, 1)
	assert.Equal(t, contextwindow.BreakdownHistory, roundTrip[0].Label)

	converted[0].Label = testLLMMutatedAgainLabel
	assert.Equal(t, contextwindow.BreakdownHistory, roundTrip[0].Label)

	assert.Nil(t, llmconv.TokenContributorsFromModel(nil))
	assert.Nil(t, llmconv.TokenContributorsToModel(nil))
}

func TestLLMUsageConversionsCloneMapsAndContributors(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown: map[string]int{contextwindow.BreakdownHistory: 1}, ContextWindow: 10, ContextTokens: 2,
		TopContributors: []model.TokenContributor{
			{Label: contextwindow.BreakdownHistory, Role: string(llm.RoleUser), Preview: "", Tokens: 1, Chars: 1},
		},
		InputTokens: 2, OutputTokens: 1,
	}

	converted := llmconv.UsageFromModel(usage)
	usage.Breakdown[contextwindow.BreakdownHistory] = 99
	usage.TopContributors[0].Label = testLLMMutatedLabel

	assert.Equal(t, 1, converted.Breakdown[contextwindow.BreakdownHistory])
	assert.Equal(t, contextwindow.BreakdownHistory, converted.TopContributors[0].Label)

	modelUsage := llmconv.UsageToModel(converted)
	converted.Breakdown[contextwindow.BreakdownHistory] = 42
	converted.TopContributors[0].Label = testLLMMutatedAgainLabel

	assert.Equal(t, 1, modelUsage.Breakdown[contextwindow.BreakdownHistory])
	assert.Equal(t, contextwindow.BreakdownHistory, modelUsage.TopContributors[0].Label)
}

func TestLLMToolDefinitionsUseBuiltinsWhenRegistryNil(t *testing.T) {
	t.Parallel()

	definitions := llmToolDefinitionsFromRegistry(nil, false)

	assert.NotEmpty(t, definitions)
}

func TestLLMToolDefinitionClonesSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"type": "object"}
	definition := llmToolDefinitionFromTool(&tool.Definition{
		Schema:           schema,
		Name:             tool.NameRead,
		Label:            jsonReadToolName,
		Description:      "read files",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         true,
	})

	definition.Schema["type"] = testLLMMutatedLabel

	assert.Equal(t, "object", schema["type"])
	assert.True(t, definition.ReadOnly)
}
