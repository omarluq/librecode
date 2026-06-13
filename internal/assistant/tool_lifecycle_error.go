package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/extension"
)

func (runtime *Runtime) emitToolLifecycleError(ctx context.Context, event *ToolEvent, err error) {
	if event == nil || err == nil {
		return
	}

	result := extension.LifecycleDispatchResult{
		Payload:         nil,
		ProviderRequest: extension.ProviderRequestMutation{Headers: nil},
		ToolCall:        extension.ToolCallMutation{Arguments: nil},
		ToolResult:      extension.ToolResultMutation{Result: nil, DetailsJSON: nil, Error: nil},
		Compaction: extension.CompactionMutation{
			Summary:          nil,
			FirstKeptEntryID: nil,
			Details:          nil,
			Cancel:           false,
		},
		Name:         string(extension.LifecycleToolResult),
		Errors:       []string{err.Error()},
		Duration:     0,
		HandlerCount: 0,
		Consumed:     false,
		Stopped:      false,
	}
	runtime.emitLifecycleDiagnostics(ctx, extension.LifecycleToolResult, &result, map[string]any{
		lifecyclepayload.ToolNameKey: event.Name,
		"lifecycle_error":            err.Error(),
		"preserved_result_bytes":     len(event.Result),
	})
}
