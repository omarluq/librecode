package transcript_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/transcript"
)

func TestFormatToolEventPersistence(t *testing.T) {
	t.Parallel()

	event := transcript.ToolEvent{
		Name:          "bash",
		ArgumentsJSON: `{"command":"false"}`,
		DetailsJSON:   `{"exit_code":1}`,
		Result:        "stderr output",
		Error:         "exit status 1",
		IsError:       true,
	}

	assert.Equal(t, stringsJoinLines(
		"tool: bash",
		"arguments:",
		`{"command":"false"}`,
		"error:",
		"exit status 1",
		"is_error: true",
		"details:",
		`{"exit_code":1}`,
		"output:",
		"stderr output",
	), transcript.FormatToolEventPersistence(&event))
}

func TestFormatToolEventDisplayOmitsStructuredErrorMarker(t *testing.T) {
	t.Parallel()

	event := transcript.ToolEvent{
		Name:          "read",
		ArgumentsJSON: "",
		DetailsJSON:   "",
		Result:        "",
		Error:         "read failed",
		IsError:       true,
	}

	assert.Equal(t, stringsJoinLines(
		"tool: read",
		"error:",
		"read failed",
	), transcript.FormatToolEventDisplay(&event))
}

func TestFormatToolEventSkipsBlankOptionalSections(t *testing.T) {
	t.Parallel()

	event := transcript.ToolEvent{
		Name:          "write",
		ArgumentsJSON: " \n\t ",
		DetailsJSON:   "",
		Result:        "\n",
		Error:         "",
		IsError:       false,
	}

	assert.Equal(t, "tool: write", transcript.FormatToolEventPersistence(&event))
}

func stringsJoinLines(lines ...string) string {
	return strings.Join(lines, "\n")
}
