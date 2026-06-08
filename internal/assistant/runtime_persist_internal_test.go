package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatToolEventIncludesErrorMarker(t *testing.T) {
	t.Parallel()

	formatted := formatToolEvent(&ToolEvent{
		Name:          jsonBashToolName,
		ArgumentsJSON: `{"command":"false"}`,
		DetailsJSON:   `{"exit_code":1}`,
		Result:        "Command exited with code 1",
		Error:         "Command exited with code 1",
		IsError:       true,
	})

	assert.Contains(t, formatted, "tool: bash")
	assert.Contains(t, formatted, "error:\nCommand exited with code 1")
	assert.Contains(t, formatted, "is_error: true")
	assert.Contains(t, formatted, "details:\n{\"exit_code\":1}")
	assert.Contains(t, formatted, "output:\nCommand exited with code 1")
}
