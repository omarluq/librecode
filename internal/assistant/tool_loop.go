package assistant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/tool"
)

func validateToolCalls(calls []toolCall) error {
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" {
			return oops.In("assistant").
				Code("responses_tool_call_missing_id").
				With("name", call.Name).
				Errorf("provider response produced a tool call without call_id")
		}
		if strings.TrimSpace(call.Name) == "" {
			return oops.In("assistant").
				Code("responses_tool_call_missing_name").
				With("call_id", call.ID).
				Errorf("provider response produced a tool call without name")
		}
	}

	return nil
}

func executeToolCalls(
	ctx context.Context,
	registry *tool.Registry,
	cwd string,
	calls []toolCall,
	onEvent func(StreamEvent),
	onToolCall func(context.Context, *ToolCallEvent) error,
	onToolResult func(context.Context, *ToolEvent) error,
) ([]any, []ToolEvent) {
	if registry == nil {
		registry = tool.NewRegistry(cwd)
	}
	outputs := make([]any, 0, len(calls))
	events := make([]ToolEvent, 0, len(calls))
	for _, call := range calls {
		event := executeOneToolCall(ctx, registry, call, onEvent, onToolCall, onToolResult)
		events = append(events, event)
		outputs = append(outputs, toolOutputForCall(call.ID, &event))
	}

	return outputs, events
}

func executeOneToolCall(
	ctx context.Context,
	registry *tool.Registry,
	call toolCall,
	onEvent func(StreamEvent),
	onToolCall func(context.Context, *ToolCallEvent) error,
	onToolResult func(context.Context, *ToolEvent) error,
) ToolEvent {
	emitToolStart(onEvent, call.Name)
	callEvent := newToolCallEvent(call)
	if err := dispatchToolCallHook(ctx, onToolCall, &callEvent); err != nil {
		event := toolLifecycleErrorEvent(callEvent, err)
		emitToolResult(onEvent, &event)

		return event
	}
	event := runToolCall(ctx, registry, callEvent)
	if err := dispatchToolResultHook(ctx, onToolResult, &event); err != nil {
		event.Error = err.Error()
		event.Result = err.Error()
	}
	emitToolResult(onEvent, &event)

	return event
}

func emitToolStart(onEvent func(StreamEvent), name string) {
	emitStreamEvent(onEvent, StreamEvent{
		ToolEvent: nil,
		Usage:     nil,
		Kind:      StreamEventToolStart,
		Text:      name,
	})
}

func emitToolResult(onEvent func(StreamEvent), event *ToolEvent) {
	emitStreamEvent(onEvent, StreamEvent{
		ToolEvent: event,
		Usage:     nil,
		Kind:      StreamEventToolResult,
		Text:      "",
	})
}

func newToolCallEvent(call toolCall) ToolCallEvent {
	return ToolCallEvent{
		Arguments:     call.Arguments,
		ID:            call.ID,
		Name:          call.Name,
		ArgumentsJSON: call.ArgumentsJSON,
	}
}

func dispatchToolCallHook(
	ctx context.Context,
	onToolCall func(context.Context, *ToolCallEvent) error,
	call *ToolCallEvent,
) error {
	if onToolCall == nil {
		return nil
	}

	return onToolCall(ctx, call)
}

func runToolCall(ctx context.Context, registry *tool.Registry, call ToolCallEvent) ToolEvent {
	result, err := registry.Execute(ctx, call.Name, call.Arguments)
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

func dispatchToolResultHook(
	ctx context.Context,
	onToolResult func(context.Context, *ToolEvent) error,
	event *ToolEvent,
) error {
	if onToolResult == nil {
		return nil
	}

	return onToolResult(ctx, event)
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

func toolOutputForCall(callID string, event *ToolEvent) map[string]any {
	output := ""
	if event != nil {
		output = toolOutputText(event.Result, event.DetailsJSON)
	}

	return map[string]any{
		jsonTypeKey:   functionCallOutputType,
		jsonCallIDKey: callID,
		jsonOutputKey: output,
	}
}

func finishTextResult(result *CompletionResult, text string) (bool, error) {
	result.Text = strings.TrimSpace(text)

	return true, nil
}

func emitStreamEvent(onEvent func(StreamEvent), event StreamEvent) {
	if onEvent != nil {
		onEvent(event)
	}
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

func toolOutputText(resultText, detailsJSON string) string {
	if strings.TrimSpace(detailsJSON) == "" {
		return resultText
	}
	trimmedResult := strings.TrimSpace(resultText)
	if trimmedResult == "" {
		return "details:\n" + detailsJSON
	}

	return trimmedResult + "\ndetails:\n" + detailsJSON
}
