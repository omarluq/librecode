package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
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
) (model.TokenUsage, bool) {
	return runtime.autoCompactAfterResponse(ctx, &postResponseAutoCompactionInput{
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

// ProviderOverflowRecoveryNilInputForTest exercises the nil-input guard for external package tests.
func (runtime *Runtime) ProviderOverflowRecoveryNilInputForTest(ctx context.Context) error {
	_, _, _, err := runtime.completeWithProviderOverflowRecovery(ctx, nil)

	return err
}

// ProviderOverflowRecoveryNilBuildForTest exercises nested nil-input guards for external package tests.
func (runtime *Runtime) ProviderOverflowRecoveryNilBuildForTest(ctx context.Context) error {
	_, _, _, err := runtime.completeWithProviderOverflowRecovery(ctx, &providerOverflowRecoveryInput{
		preparation:     nil,
		build:           nil,
		compactionEntry: nil,
		onRetry:         nil,
	})

	return err
}

// ProviderOverflowRecoveryNonContextErrorForTest exercises the non-overflow passthrough path.
func (runtime *Runtime) ProviderOverflowRecoveryNonContextErrorForTest(ctx context.Context) error {
	auth := model.RequestAuth{Headers: nil, APIKey: "test", Error: "", OK: true}
	_, _, _, err := runtime.completeWithProviderOverflowRecovery(ctx, &providerOverflowRecoveryInput{
		preparation: &completionRequestPreparationInput{
			selectedModel: nil,
			onEvent:       nil,
			auth:          &auth,
			sessionID:     "",
			cwd:           "",
			prompt:        "",
			userEntryID:   "",
		},
		build: &contextRequestBuild{
			Context: nil,
			Request: &CompletionRequest{
				OnEvent:           nil,
				OnProviderObserve: nil,
				OnProviderRequest: nil,
				OnToolCall:        nil,
				OnToolResult:      nil,
				ToolRegistry:      nil,
				ExecuteTools:      nil,
				SessionID:         "",
				SystemPrompt:      "",
				ThinkingLevel:     "",
				CWD:               "",
				Auth:              auth,
				Messages:          nil,
				Usage: model.TokenUsage{
					Breakdown:       nil,
					TopContributors: nil,
					ContextWindow:   0,
					ContextTokens:   0,
					InputTokens:     0,
					OutputTokens:    0,
				},
				Model: model.Model{
					ThinkingLevelMap: nil,
					Headers:          nil,
					Compat:           nil,
					Provider:         "",
					ID:               "",
					Name:             "",
					API:              "",
					BaseURL:          "",
					Input:            nil,
					Cost: model.Cost{
						Input:      0,
						Output:     0,
						CacheRead:  0,
						CacheWrite: 0,
					},
					ContextWindow: 0,
					MaxTokens:     0,
					Reasoning:     false,
				},
				ProviderAttempt: 0,
				DisableTools:    false,
			},
			Budget: contextBudget{
				InputTokens:       0,
				ContextWindow:     0,
				UsableInput:       0,
				OutputReserve:     0,
				ToolSchemaReserve: 0,
				ProviderReserve:   0,
				SafetyMargin:      0,
			},
		},
		compactionEntry: nil,
		onRetry:         nil,
	})

	return err
}
