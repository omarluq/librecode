package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/database"
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

// AutoCompactAfterResponseForTest exposes post-response auto-compaction for external package tests.
func (runtime *Runtime) AutoCompactAfterResponseForTest(
	ctx context.Context,
	onEvent func(StreamEvent),
	sessionID string,
	cwd string,
	parentEntryID string,
) {
	runtime.autoCompactAfterResponse(ctx, &postResponseAutoCompactionInput{
		onEvent:       onEvent,
		sessionID:     sessionID,
		cwd:           cwd,
		parentEntryID: parentEntryID,
	})
}

// EmitPostResponseAutoCompactionErrorForTest exposes error event formatting for external package tests.
func (runtime *Runtime) EmitPostResponseAutoCompactionErrorForTest(
	ctx context.Context,
	onEvent func(StreamEvent),
	err error,
) {
	runtime.emitPostResponseAutoCompactionError(ctx, onEvent, err)
}

// AutoCompactionMessageForTest exposes compaction notice formatting for external package tests.
// The contextBudget field values are arbitrary; tests use them only to exercise
// compactionMessage formatting, not to assert semantic budget calculations.
func AutoCompactionMessageForTest(entry *database.EntryEntity) string {
	return compactionMessage("context auto-compacted", contextBudget{
		InputTokens:       12,
		ContextWindow:     0,
		UsableInput:       10,
		OutputReserve:     0,
		ToolSchemaReserve: 0,
		ProviderReserve:   0,
		SafetyMargin:      0,
	}, entry)
}
