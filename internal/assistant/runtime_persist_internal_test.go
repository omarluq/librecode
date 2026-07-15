package assistant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/tool"
)

const (
	runtimePersistTestArgsJSON = `{"path":"a.go"}`
	runtimePersistToolCallID   = "call-1"
)

func TestFormatToolEventIncludesErrorMarker(t *testing.T) {
	t.Parallel()

	formatted := formatToolEvent(&ToolEvent{
		CallID:       "",
		ParentCallID: "",
		Sequence:     0,

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

func TestPartialPromptProgressReplacesPendingToolWithResult(t *testing.T) {
	t.Parallel()

	progress := newPartialPromptProgress(nil)
	progress.record(StreamEvent{
		ToolCallEvent: runtimePersistTestToolCall(),
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          StreamEventToolStart,
		Text:          jsonReadToolName,
	})
	progress.record(StreamEvent{
		ToolCallEvent: nil,
		ToolEvent:     runtimePersistTestToolResult(),
		Usage:         nil,
		Kind:          StreamEventToolResult,
		Text:          "",
	})

	assert.Empty(t, progress.syntheticToolFailureEvents(context.Canceled))
}

func TestPartialPromptProgressResetClearsPendingTools(t *testing.T) {
	t.Parallel()

	progress := newPartialPromptProgress(nil)
	progress.record(StreamEvent{
		ToolCallEvent: runtimePersistTestToolCall(),
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          StreamEventToolStart,
		Text:          jsonReadToolName,
	})

	progress.reset()

	assert.Empty(t, progress.syntheticToolFailureEvents(context.Canceled))
}

func runtimePersistTestToolCall() *ToolCallEvent {
	return &ToolCallEvent{
		ParentCallID: "",
		Sequence:     0,

		ArgumentsJSON: runtimePersistTestArgsJSON,
		ID:            runtimePersistToolCallID,
		Name:          jsonReadToolName,
		Arguments:     tool.EmptyArguments(),
	}
}

func runtimePersistTestToolResult() *ToolEvent {
	return &ToolEvent{
		CallID:       "",
		ParentCallID: "",
		Sequence:     0,

		Name:          jsonReadToolName,
		ArgumentsJSON: runtimePersistTestArgsJSON,
		DetailsJSON:   "",
		Result:        "ok",
		Error:         "",
		IsError:       false,
	}
}
