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
