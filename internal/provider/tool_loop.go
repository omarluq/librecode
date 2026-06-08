package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/tool"
)

func validateToolCalls(calls []ToolCall) error {
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
	request *CompletionRequest,
	calls []ToolCall,
) ([]any, []ToolEvent, error) {
	var events []ToolEvent
	var err error
	if request != nil && request.ExecuteTools != nil {
		events, err = request.ExecuteTools(ctx, calls, request.OnEvent)
	} else {
		events = executeToolCallsLocally(
			ctx,
			requestToolRegistry(request),
			calls,
			requestEventHandler(request),
			requestToolCallHook(request),
			requestToolResultHook(request),
		)
	}
	if err != nil {
		return nil, nil, err
	}
	outputs := make([]any, 0, len(calls))
	for index := range calls {
		outputs = append(outputs, toolOutputForCall(calls[index].ID, &events[index]))
	}

	return outputs, events, nil
}

func executeToolCallsLocally(
	ctx context.Context,
	registry *tool.Registry,
	calls []ToolCall,
	onEvent func(StreamEvent),
	onToolCall func(context.Context, *ToolCallEvent) error,
	onToolResult func(context.Context, *ToolEvent) error,
) []ToolEvent {
	if registry == nil {
		registry = tool.NewRegistry("")
	}
	events := make([]ToolEvent, 0, len(calls))
	for _, call := range calls {
		events = append(events, executeOneToolCall(ctx, registry, call, onEvent, onToolCall, onToolResult))
	}

	return events
}

func requestToolRegistry(request *CompletionRequest) *tool.Registry {
	if request == nil || request.ToolRegistry == nil {
		return tool.NewRegistry(requestCWD(request))
	}

	return request.ToolRegistry
}

func requestCWD(request *CompletionRequest) string {
	if request == nil {
		return ""
	}

	return request.CWD
}

func requestEventHandler(request *CompletionRequest) func(StreamEvent) {
	if request == nil {
		return nil
	}

	return request.OnEvent
}

func requestToolCallHook(request *CompletionRequest) func(context.Context, *ToolCallEvent) error {
	if request == nil {
		return nil
	}

	return request.OnToolCall
}

func requestToolResultHook(request *CompletionRequest) func(context.Context, *ToolEvent) error {
	if request == nil {
		return nil
	}

	return request.OnToolResult
}

func executeOneToolCall(
	ctx context.Context,
	registry *tool.Registry,
	call ToolCall,
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

func newToolCallEvent(call ToolCall) ToolCallEvent {
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

	return ToolEventFromResult(call, result, err)
}

// ToolEventFromResult converts a local tool execution result into a provider-facing tool event.
func ToolEventFromResult(call ToolCallEvent, result tool.Result, err error) ToolEvent {
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
