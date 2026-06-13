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

	var result extension.LifecycleDispatchResult

	result.Name = string(extension.LifecycleToolResult)
	result.Errors = []string{err.Error()}
	runtime.emitLifecycleDiagnostics(ctx, extension.LifecycleToolResult, &result, map[string]any{
		lifecyclepayload.ToolNameKey: event.Name,
		"lifecycle_error":            err.Error(),
		"preserved_result_bytes":     len(event.Result),
	})
}
