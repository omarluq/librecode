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
