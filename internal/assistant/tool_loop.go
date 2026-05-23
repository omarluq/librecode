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
	onToolCall func(context.Context, ToolCallEvent),
	onToolResult func(context.Context, *ToolEvent),
) ([]any, []ToolEvent) {
	if registry == nil {
		registry = tool.NewRegistry(cwd)
	}
	outputs := make([]any, 0, len(calls))
	events := make([]ToolEvent, 0, len(calls))
	for _, call := range calls {
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: nil,
			Usage:     nil,
			Kind:      StreamEventToolStart,
			Text:      call.Name,
		})
		if onToolCall != nil {
			onToolCall(ctx, ToolCallEvent{
				Arguments:     call.Arguments,
				ID:            call.ID,
				Name:          call.Name,
				ArgumentsJSON: call.ArgumentsJSON,
			})
		}
		result, err := registry.Execute(ctx, call.Name, call.Arguments)
		resultText := result.Text()
		detailsJSON := encodeToolDetails(result.Details)
		event := ToolEvent{
			Name:          call.Name,
			ArgumentsJSON: call.ArgumentsJSON,
			DetailsJSON:   detailsJSON,
			Result:        resultText,
			Error:         "",
		}
		if err != nil {
			event.Error = err.Error()
			resultText = err.Error()
		}
		if strings.TrimSpace(resultText) == "" {
			resultText = "(tool returned no text output)"
		}
		event.Result = resultText
		events = append(events, event)
		if onToolResult != nil {
			onToolResult(ctx, &event)
		}
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: &event,
			Usage:     nil,
			Kind:      StreamEventToolResult,
			Text:      "",
		})
		outputs = append(outputs, map[string]any{
			jsonTypeKey:   functionCallOutputType,
			jsonCallIDKey: call.ID,
			jsonOutputKey: toolOutputText(resultText, detailsJSON),
		})
	}

	return outputs, events
}

func finishTextResult(result *CompletionResult, text, emptyCode string) (bool, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false, emptyProviderResponseError(emptyCode)
	}
	result.Text = trimmed

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
