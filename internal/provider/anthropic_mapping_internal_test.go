package provider

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestAnthropicThinkingEffortMappings(t *testing.T) {
	t.Parallel()

	mappedHigh := thinkingHigh
	request := testCompletionRequestAuth("sk-ant-api03-secret")
	setTestThinkingMap(request, thinkingLow, mappedHigh)
	setTestRequestThinkingLevel(request, thinkingLow)
	assert.Equal(t, thinkingHigh, anthropicThinkingEffort(request))

	request.Request.Model.ThinkingLevelMap = nil

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
			setTestRequestThinkingLevel(request, testCase.level)
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
	setTestRequestReasoning(request, false)
	assert.Empty(t, anthropicBetaFeatures(request))

	setTestRequestReasoning(request, true)
	setTestRequestModelID(request, "claude-sonnet-4-5")
	assert.Contains(t, anthropicBetaFeatures(request), "interleaved-thinking-2025-05-14")

	setTestRequestModelID(request, testAdaptiveAnthropicModelID)
	assert.Empty(t, anthropicBetaFeatures(request))
}

func TestAnthropicToolArgumentsHandlesMalformedAndScalarInput(t *testing.T) {
	t.Parallel()

	arguments, argumentsJSON := anthropicToolArguments(func() {})
	assert.Equal(t, map[string]any{}, testutil.ToolArgumentFields(arguments))
	assert.JSONEq(t, "{}", argumentsJSON)

	arguments, argumentsJSON = anthropicToolArguments("plain")
	assert.Equal(t, map[string]any{}, testutil.ToolArgumentFields(arguments))
	assert.JSONEq(t, `"plain"`, argumentsJSON)
}

func TestParseAnthropicResultMapsFinishReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want     llm.FinishReason
		name     string
		body     string
		wantText string
	}{
		{
			name:     "max tokens",
			body:     anthropicResponseJSON("max_tokens", testProviderPartialText, nil),
			want:     llm.FinishReasonLength,
			wantText: "",
		},
		{
			name:     "context exceeded",
			body:     anthropicResponseJSON("model_context_window_exceeded", testProviderPartialText, nil),
			want:     llm.FinishReasonLength,
			wantText: "",
		},
		{
			name: "refusal keeps provider text and drops tool calls",
			body: anthropicResponseJSON(anthropicRefusalReason, testProviderPartialText, &anthropicStopDetails{
				Type:        anthropicRefusalReason,
				Category:    "cyber",
				Explanation: testProviderDeclined,
			}),
			want:     llm.FinishReasonRefusal,
			wantText: testProviderPartialText,
		},
		{
			name: "refusal without provider text uses stop details",
			body: anthropicResponseJSON(anthropicRefusalReason, "", &anthropicStopDetails{
				Type:        anthropicRefusalReason,
				Category:    "cyber",
				Explanation: testProviderDeclined,
			}),
			want:     llm.FinishReasonRefusal,
			wantText: "The model refused the request (cyber): declined",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseAnthropicStream(strings.NewReader(anthropicResponseStream(test.body)), nil)

			require.NoError(t, err)
			assert.Equal(t, test.want, result.FinishReason)

			if test.wantText != "" {
				assert.Equal(t, test.wantText, result.Text)
			}

			if test.want == llm.FinishReasonRefusal {
				assert.Empty(t, result.ToolCalls)
			}
		})
	}
}

func TestAnthropicAssistantToolMessageMapsProviderNames(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("anthropic-claude", "subscription-access-token")
	message := anthropicAssistantToolMessage(request, []ToolCall{{
		Arguments:     testutil.ToolArguments(map[string]any{jsonPathKey: testToolPath}),
		Metadata:      nil,
		ID:            testAnthropicToolUseID,
		Name:          jsonReadToolName,
		ArgumentsJSON: testToolArgumentsJSON,
	}})

	blocks, ok := message[jsonContentKey].([]map[string]any)
	require.True(t, ok)
	require.Len(t, blocks, 1)
	assert.Equal(t, anthropicToolUseType, blocks[0][jsonTypeKey])
	assert.Equal(t, anthropicReadToolName, blocks[0][jsonToolNameKey])
}

func TestAnthropicMessagesAndRoles(t *testing.T) {
	t.Parallel()

	request := emptyCompletionRequest()
	setTestRequestMessages(request, mixedReplayMessages())

	converted := anthropicMessages(request.Request.Messages)

	assert.Len(t, converted, 7)
	assert.JSONEq(t, jsonString(jsonUserRole), jsonString(converted[0][jsonRoleKey]))
	assert.JSONEq(t, jsonString(jsonAssistantRole), jsonString(converted[1][jsonRoleKey]))

	mapped, ok := anthropicRole(llm.RoleTool)
	assert.False(t, ok)
	assert.Empty(t, mapped)
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
		{name: "unknown", input: "Unknown", want: "Unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, anthropicLocalToolName(test.input))
		})
	}
}
