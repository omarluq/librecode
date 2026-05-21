//nolint:testpackage // Tests exercise unexported text fallback tool-call helpers.
package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextToolCallsFromTextParsesXMLStyleToolUse(t *testing.T) {
	t.Parallel()

	text := `<tool_use>
<tool_name>Read</tool_name>
<file_path>/tmp/README.md</file_path>
</tool_use>`

	calls := textToolCallsFromText(text)
	require.Len(t, calls, 1)
	assert.Equal(t, "read", calls[0].Name)
	assert.Equal(t, "/tmp/README.md", calls[0].Arguments[jsonPathKey])
	assert.Equal(t, `{"path":"/tmp/README.md"}`, calls[0].ArgumentsJSON)
	assert.True(t, calls[0].TextFallback)
}

func TestTextToolCallsFromTextMapsCommonFields(t *testing.T) {
	t.Parallel()

	text := `<tool_use><name>shell</name><cmd>pwd</cmd></tool_use>`

	calls := textToolCallsFromText(text)
	require.Len(t, calls, 1)
	assert.Equal(t, "bash", calls[0].Name)
	assert.Equal(t, "pwd", calls[0].Arguments["command"])
}

func TestTextToolCallsFromTextIgnoresUnknownTools(t *testing.T) {
	t.Parallel()

	calls := textToolCallsFromText(`<tool_use><tool_name>unknown</tool_name></tool_use>`)

	assert.Empty(t, calls)
}

func TestTextToolCallsFromTextParsesMultipleAndEscapedValues(t *testing.T) {
	t.Parallel()

	text := `<tool_use>
<tool_name>Read</tool_name>
<file_path>README.md</file_path>
</tool_use>
<tool_use>
<tool_name>bash</tool_name>
<command>printf &quot;hello&quot;</command>
</tool_use>`

	calls := textToolCallsFromText(text)
	require.Len(t, calls, 2)
	assert.Equal(t, "text_tool_call_1", calls[0].ID)
	assert.Equal(t, "read", calls[0].Name)
	assert.Equal(t, "README.md", calls[0].Arguments[jsonPathKey])
	assert.Equal(t, "text_tool_call_2", calls[1].ID)
	assert.Equal(t, "bash", calls[1].Name)
	assert.Equal(t, `printf "hello"`, calls[1].Arguments[jsonCommandKey])
}

func TestTextToolCallsFromTextMapsToolNamesAndArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		markup        string
		expectedTool  string
		expectedKey   string
		expectedValue string
	}{
		{
			name:          "write content",
			markup:        writeTextToolMarkup(),
			expectedTool:  jsonWriteToolName,
			expectedKey:   jsonPathKey,
			expectedValue: "out.txt",
		},
		{
			name:          "edit old text",
			markup:        editTextToolMarkup(),
			expectedTool:  jsonEditToolName,
			expectedKey:   jsonOldTextKey,
			expectedValue: "old",
		},
		{
			name:          "grep pattern",
			markup:        grepTextToolMarkup(),
			expectedTool:  jsonGrepToolName,
			expectedKey:   jsonPatternKey,
			expectedValue: "TODO",
		},
		{
			name:          "ls path",
			markup:        `<tool_use><tool_name>list_directory</tool_name><filepath>.</filepath></tool_use>`,
			expectedTool:  "ls",
			expectedKey:   jsonPathKey,
			expectedValue: ".",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			calls := textToolCallsFromText(testCase.markup)

			require.Len(t, calls, 1)
			assert.Equal(t, testCase.expectedTool, calls[0].Name)
			assert.Equal(t, testCase.expectedValue, calls[0].Arguments[testCase.expectedKey])
		})
	}
}

func writeTextToolMarkup() string {
	return `<tool_use><tool_name>create</tool_name>` +
		`<file>out.txt</file><content>hello</content></tool_use>`
}

func editTextToolMarkup() string {
	return `<tool_use><tool_name>replace</tool_name>` +
		`<old-text>old</old-text><new-text>new</new-text></tool_use>`
}

func grepTextToolMarkup() string {
	return `<tool_use><tool_name>search</tool_name>` +
		`<pattern>TODO</pattern><ignore-case>true</ignore-case></tool_use>`
}

func TestTextToolResultPromptUsesErrorsAndEmptyFallback(t *testing.T) {
	t.Parallel()

	prompt := textToolResultPrompt([]ToolEvent{
		{Name: "read", ArgumentsJSON: `{}`, DetailsJSON: "", Result: "", Error: "missing file"},
		{Name: "bash", ArgumentsJSON: `{}`, DetailsJSON: "", Result: "   ", Error: ""},
	})

	assert.Contains(t, prompt, "Tool result for read:\nmissing file")
	assert.Contains(t, prompt, "Tool result for bash:\n(tool returned no text output)")
}
