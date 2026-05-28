package assistant

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/omarluq/librecode/internal/extension"
)

const (
	lifecycleHookCountKey  = "hook_count"
	lifecycleDurationMsKey = "duration_ms"
	lifecycleErrorsKey     = "hook_errors"
)

func (runtime *Runtime) emitLifecycleDiagnostics(
	ctx context.Context,
	name extension.LifecycleEventName,
	result *extension.LifecycleDispatchResult,
	extra map[string]any,
) {
	payload := map[string]any{
		"event":                string(name),
		lifecycleHookCountKey:  result.HandlerCount,
		lifecycleDurationMsKey: durationMilliseconds(result.Duration),
	}
	if len(result.Errors) > 0 {
		payload[lifecycleErrorsKey] = append([]string{}, result.Errors...)
	}
	for key, value := range extra {
		payload[key] = value
	}

	runtime.emit(ctx, string(name)+"_diagnostic", payload)
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
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
		lifecycleAPIKey:      request.Model.API,
		lifecycleAttemptKey:  attempt,
		jsonModelKey:         request.Model.ID,
		lifecycleProviderKey: request.Model.Provider,
		jsonSessionIDKey:     request.SessionID,
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
	diagnostics[jsonContextTokensKey] = result.Usage.ContextTokens
	diagnostics[jsonContextWindowKey] = result.Usage.ContextWindow
	diagnostics[jsonInputTokensKey] = result.Usage.InputTokens
	diagnostics[jsonOutputTokensKey] = result.Usage.OutputTokens

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
	diagnostics[lifecycleErrorKey] = err.Error()
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
		"call_id":       call.ID,
		jsonToolNameKey: call.Name,
		"argument_keys": sortedAnyMapKeys(call.Arguments),
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
		jsonToolNameKey:       event.Name,
		"has_error":           event.Error != "",
		"result_bytes":        len(event.Result),
		"details_json_bytes":  len(event.DetailsJSON),
		lifecycleToolErrorKey: event.Error,
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
