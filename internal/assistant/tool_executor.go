package assistant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

func (runtime *Runtime) executeProviderToolCalls(
	registry *tool.Registry,
) ToolExecutor {
	return func(
		ctx context.Context,
		calls []ToolCall,
		onEvent func(StreamEvent),
	) ([]ToolEvent, error) {
		if registry == nil {
			return nil, oops.In("assistant").Code("tool_registry_missing").Errorf("tool registry is not configured")
		}

		events := make([]ToolEvent, 0, len(calls))
		for _, call := range calls {
			events = append(events, runtime.executeProviderToolCall(ctx, registry, call, onEvent))
		}

		return events, nil
	}
}

func (runtime *Runtime) executeProviderToolCall(
	ctx context.Context,
	registry *tool.Registry,
	call ToolCall,
	onEvent func(StreamEvent),
) ToolEvent {
	emitProviderToolStart(onEvent, call.Name)

	callEvent := ToolCallEvent{
		Arguments:     call.Arguments,
		ID:            call.ID,
		Name:          call.Name,
		ArgumentsJSON: call.ArgumentsJSON,
	}
	if err := runtime.dispatchToolCallLifecycle(ctx, &callEvent); err != nil {
		event := toolLifecycleErrorEvent(callEvent, err)
		emitProviderToolResult(onEvent, &event)

		return event
	}

	result, err := registry.Execute(ctx, callEvent.Name, callEvent.Arguments)

	event := toolEventFromResult(callEvent, result, err)
	if lifecycleErr := runtime.dispatchToolResultLifecycle(ctx, &event); lifecycleErr != nil {
		runtime.emitToolLifecycleError(ctx, &event, lifecycleErr)
	}

	emitProviderToolResult(onEvent, &event)

	return event
}

func toolLifecycleErrorEvent(call ToolCallEvent, err error) ToolEvent {
	return ToolEvent{
		Name:          call.Name,
		ArgumentsJSON: call.ArgumentsJSON,
		DetailsJSON:   "",
		Result:        err.Error(),
		Error:         err.Error(),
		IsError:       true,
	}
}

func toolEventFromResult(call ToolCallEvent, result tool.Result, err error) ToolEvent {
	resultText := result.Text()
	detailsJSON := encodeToolDetails(result.Details)

	if err != nil {
		resultText = err.Error()
	}

	if strings.TrimSpace(resultText) == "" {
		resultText = "(tool returned no text output)"
	}

	event := ToolEvent{
		Name:          call.Name,
		ArgumentsJSON: call.ArgumentsJSON,
		DetailsJSON:   detailsJSON,
		Result:        resultText,
		Error:         "",
		IsError:       false,
	}
	if err != nil {
		event.Error = err.Error()
		event.IsError = true
	}

	return event
}

func encodeToolDetails(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}

	encoded, err := json.Marshal(details)
	if err != nil {
		return ""
	}

	return string(encoded)
}

func emitProviderToolStart(onEvent func(StreamEvent), name string) {
	if onEvent == nil {
		return
	}

	onEvent(StreamEvent{ToolEvent: nil, Usage: nil, Kind: StreamEventToolStart, Text: name})
}

func emitProviderToolResult(onEvent func(StreamEvent), event *ToolEvent) {
	if onEvent == nil {
		return
	}

	onEvent(StreamEvent{ToolEvent: event, Usage: nil, Kind: StreamEventToolResult, Text: ""})
}

func llmToolResultFromToolEvent(event *ToolEvent) llm.ToolResult {
	if event == nil {
		return llm.ToolResult{
			Metadata:      nil,
			ToolCallID:    "",
			ArgumentsJSON: "",
			Name:          "",
			Error:         "",
			Content:       nil,
			IsError:       false,
		}
	}

	return llm.ToolResult{
		Metadata:      nil,
		ToolCallID:    "",
		ArgumentsJSON: event.ArgumentsJSON,
		Name:          event.Name,
		Error:         event.Error,
		Content:       []llm.Part{llm.TextPart(event.Result)},
		IsError:       event.IsError,
	}
}
