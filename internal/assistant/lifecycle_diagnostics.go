package assistant

import (
	"context"
	"fmt"
	"sort"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/extension"
)

func (runtime *Runtime) emitLifecycleDiagnostics(
	ctx context.Context,
	name extension.LifecycleEventName,
	result *extension.LifecycleDispatchResult,
	extra map[string]any,
) {
	payload := lifecyclepayload.Diagnostic(
		string(name),
		result.HandlerCount,
		result.Duration,
		result.Errors,
		extra,
	)
	runtime.emit(ctx, string(name)+"_diagnostic", payload)
}

func providerHookDiagnostics(input providerHookInput, output providerHookOutput) map[string]any {
	diagnostics := providerBaseDiagnostics(input.Request, input.Attempt)
	diagnostics["mutated_header_count"] = changedStringMapCount(input.Headers, output.Headers)
	diagnostics["mutated_payload_key_count"] = changedAnyMapCount(input.Payload, output.Payload)

	return diagnostics
}

func providerBaseDiagnostics(request *CompletionRequest, attempt int) map[string]any {
	if request == nil {
		return map[string]any{}
	}

	return map[string]any{
		lifecyclepayload.APIKey:       request.Model.API,
		lifecyclepayload.AttemptKey:   attempt,
		lifecyclepayload.ModelKey:     request.Model.ID,
		lifecyclepayload.ProviderKey:  request.Model.Provider,
		lifecyclepayload.SessionIDKey: request.SessionID,
	}
}

func providerResponseDiagnostics(
	request *CompletionRequest,
	attempt int,
	result *CompletionResult,
) map[string]any {
	if result == nil {
		return map[string]any{}
	}
	diagnostics := providerBaseDiagnostics(request, attempt)
	diagnostics["response_text_bytes"] = len(result.Text)
	diagnostics["thinking_count"] = len(result.Thinking)
	diagnostics["tool_event_count"] = len(result.ToolEvents)
	diagnostics[lifecyclepayload.ContextTokensKey] = result.Usage.ContextTokens
	diagnostics[lifecyclepayload.ContextWindowKey] = result.Usage.ContextWindow
	diagnostics[lifecyclepayload.InputTokensKey] = result.Usage.InputTokens
	diagnostics[lifecyclepayload.OutputTokensKey] = result.Usage.OutputTokens

	return diagnostics
}

func providerErrorDiagnostics(
	request *CompletionRequest,
	attempt int,
	err error,
) map[string]any {
	if err == nil {
		return map[string]any{}
	}

	diagnostics := providerBaseDiagnostics(request, attempt)
	diagnostics[lifecyclepayload.ErrorKey] = err.Error()
	diagnostics["retryable"] = ShouldRetryModelError(err)
	if code, ok := providerErrorCode(err); ok {
		diagnostics["error_code"] = code
	}
	if status, ok := providerErrorStatus(err); ok {
		diagnostics["status"] = status
	}

	return diagnostics
}

func toolCallDiagnostics(call *ToolCallEvent) map[string]any {
	return map[string]any{
		"call_id":                    call.ID,
		lifecyclepayload.ToolNameKey: call.Name,
		"argument_keys":              sortedAnyMapKeys(call.Arguments),
	}
}

func sortedAnyMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func toolResultDiagnostics(event *ToolEvent) map[string]any {
	return map[string]any{
		lifecyclepayload.ToolNameKey:  event.Name,
		"has_error":                   event.Error != "",
		"result_bytes":                len(event.Result),
		"details_json_bytes":          len(event.DetailsJSON),
		lifecyclepayload.ToolErrorKey: event.Error,
	}
}

func changedStringMapCount(before, after map[string]string) int {
	changed := 0
	seen := map[string]struct{}{}
	for key, beforeValue := range before {
		seen[key] = struct{}{}
		if after[key] != beforeValue {
			changed++
		}
	}
	for key := range after {
		if _, ok := seen[key]; !ok {
			changed++
		}
	}

	return changed
}

func changedAnyMapCount(before, after map[string]any) int {
	changed := 0
	seen := map[string]struct{}{}
	for key, beforeValue := range before {
		seen[key] = struct{}{}
		if fmt.Sprint(after[key]) != fmt.Sprint(beforeValue) {
			changed++
		}
	}
	for key := range after {
		if _, ok := seen[key]; !ok {
			changed++
		}
	}

	return changed
}
