//nolint:testpackage // Tests exercise unexported Anthropic tool helpers.
package assistant

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAnthropicResultExtractsNativeToolUse(t *testing.T) {
	t.Parallel()

	content := []byte(`{
		"content": [
			{"type":"tool_use","id":"toolu_1","name":"read","input":{"path":"README.md"}}
		],
		"usage": {"input_tokens": 12, "output_tokens": 3}
	}`)

	result, err := parseAnthropicResult(content)
	require.NoError(t, err)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "toolu_1", result.ToolCalls[0].ID)
	assert.Equal(t, "read", result.ToolCalls[0].Name)
	assert.Equal(t, "README.md", result.ToolCalls[0].Arguments[jsonPathKey])
	assert.Equal(t, 12, result.Usage.InputTokens)
	assert.Equal(t, 3, result.Usage.OutputTokens)
}

func TestAnthropicPayloadIncludesTools(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	payload := anthropicPayload(request, nil)

	tools, ok := payload["tools"].([]map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, tools)
	encoded, err := json.Marshal(tools)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"input_schema"`)
	assert.Contains(t, string(encoded), `"read"`)
}

func TestAnthropicToolResultMessageUsesToolUseID(t *testing.T) {
	t.Parallel()

	message := anthropicToolResultMessage(
		[]toolCall{{Arguments: nil, ID: "toolu_1", Name: jsonReadToolName, ArgumentsJSON: `{}`, TextFallback: false}},
		[]ToolEvent{{Name: jsonReadToolName, ArgumentsJSON: `{}`, DetailsJSON: "", Result: "ok", Error: ""}},
	)

	blocks, ok := message[jsonContentKey].([]map[string]any)
	require.True(t, ok)
	require.Len(t, blocks, 1)
	assert.Equal(t, "toolu_1", blocks[0]["tool_use_id"])
	assert.Equal(t, "ok", blocks[0][jsonContentKey])
}
