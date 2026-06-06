package assistant

import (
	"context"
)

// DispatchToolCallLifecycleForTest exposes tool call lifecycle dispatch for external package tests.
func (runtime *Runtime) DispatchToolCallLifecycleForTest(ctx context.Context, call *ToolCallEvent) error {
	return runtime.dispatchToolCallLifecycle(ctx, call)
}

// DispatchToolResultLifecycleForTest exposes tool result lifecycle dispatch for external package tests.
func (runtime *Runtime) DispatchToolResultLifecycleForTest(ctx context.Context, event *ToolEvent) error {
	return runtime.dispatchToolResultLifecycle(ctx, event)
}

// ShouldAutoCompactAfterResponseForTest exposes post-response threshold policy for external package tests.
func ShouldAutoCompactAfterResponseForTest(usageInput, usableInput, contextWindow int) bool {
	return shouldAutoCompactAfterResponse(contextBudget{
		InputTokens:       usageInput,
		ContextWindow:     contextWindow,
		UsableInput:       usableInput,
		OutputReserve:     0,
		ToolSchemaReserve: 0,
		ProviderReserve:   0,
		SafetyMargin:      0,
	})
}
