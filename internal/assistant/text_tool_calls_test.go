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

func TestTextToolCallsFromTextPreservesMultilineWriteContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		markup    string
		wantValue string
	}{
		{
			name:      "content tag",
			markup:    writeToolMarkupWithField(jsonContentKey, "line one\n\n\tindented line\nline three\n"),
			wantValue: "line one\n\n\tindented line\nline three\n",
		},
		{
			name:      "file content tag",
			markup:    writeToolMarkupWithField("file_content", "package main\n\n\tindented line\nline three\n"),
			wantValue: "package main\n\n\tindented line\nline three\n",
		},
		{
			name:      "new content tag",
			markup:    writeToolMarkupWithField("new_content", "# README\n\n\tindented line\nline three\n"),
			wantValue: "# README\n\n\tindented line\nline three\n",
		},
		{
			name:      "code tag",
			markup:    writeToolMarkupWithField("code", "func main\n\n\tindented line\nline three\n"),
			wantValue: "func main\n\n\tindented line\nline three\n",
		},
		{
			name: "json input object",
			markup: `<tool_use>
<tool_name>Write</tool_name>
<input>{"path":"hello.txt","content":"line one\n\n\tindented line\nline three\n"}</input>
</tool_use>`,
			wantValue: "line one\n\n\tindented line\nline three\n",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			calls := textToolCallsFromText(testCase.markup)

			require.Len(t, calls, 1)
			assert.Equal(t, jsonWriteToolName, calls[0].Name)
			assert.Equal(t, "hello.txt", calls[0].Arguments[jsonPathKey])
			assert.Equal(t, testCase.wantValue, calls[0].Arguments[jsonContentKey])
		})
	}
}

func writeToolMarkupWithField(fieldName, value string) string {
	return `<tool_use>
<tool_name>Write</tool_name>
<file_path>hello.txt</file_path>
<` + fieldName + `>` + value + `</` + fieldName + `>
</tool_use>`
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
		{
			name:          "ast path",
			markup:        `<tool_use><tool_name>ast</tool_name><filepath>main.go</filepath></tool_use>`,
			expectedTool:  jsonASTToolName,
			expectedKey:   jsonPathKey,
			expectedValue: "main.go",
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
		{Name: jsonReadToolName, ArgumentsJSON: `{}`, DetailsJSON: "", Result: "", Error: "missing file"},
		{Name: jsonBashToolName, ArgumentsJSON: `{}`, DetailsJSON: "", Result: "   ", Error: ""},
	})

	assert.Contains(t, prompt, "Tool result for read:\nmissing file")
	assert.Contains(t, prompt, "Tool result for bash:\n(tool returned no text output)")
}
