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
		CallID:        "",
		ParentCallID:  "",
		Name:          "bash",
		ArgumentsJSON: `{"command":"false"}`,
		DetailsJSON:   `{"exit_code":1}`,
		Result:        "stderr output",
		Error:         "exit status 1",
		Sequence:      0,
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

func TestFormatToolEventPersistenceIncludesTraceIdentity(t *testing.T) {
	t.Parallel()

	event := transcript.ToolEvent{
		CallID: "outer/2", ParentCallID: "outer", Name: "read", ArgumentsJSON: "", DetailsJSON: "",
		Result: "ok", Error: "", Sequence: 2, IsError: false,
	}

	formatted := transcript.FormatToolEventPersistence(&event)
	assert.Contains(t, formatted, "call_id: outer/2")
	assert.Contains(t, formatted, "parent_call_id: outer")
	assert.Contains(t, formatted, "sequence: 2")
}

func TestFormatToolEventDisplayOmitsStructuredErrorMarker(t *testing.T) {
	t.Parallel()

	event := transcript.ToolEvent{
		CallID:        "",
		ParentCallID:  "",
		Name:          "read",
		ArgumentsJSON: "",
		DetailsJSON:   "",
		Result:        "",
		Error:         "read failed",
		Sequence:      0,
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
		CallID:        "",
		ParentCallID:  "",
		Name:          "write",
		ArgumentsJSON: " \n\t ",
		DetailsJSON:   "",
		Result:        "\n",
		Error:         "",
		Sequence:      0,
		IsError:       false,
	}

	assert.Equal(t, "tool: write", transcript.FormatToolEventPersistence(&event))
}

func stringsJoinLines(lines ...string) string {
	return strings.Join(lines, "\n")
}
