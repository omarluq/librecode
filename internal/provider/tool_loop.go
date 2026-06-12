package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
)

func validateToolCalls(calls []ToolCall) error {
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" {
			return oops.In("provider").
				Code("responses_tool_call_missing_id").
				With("name", call.Name).
				Errorf("provider response produced a tool call without call_id")
		}
		if strings.TrimSpace(call.Name) == "" {
			return oops.In("provider").
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
	if request == nil || request.ExecuteTools == nil {
		return nil, nil, oops.In("provider").
			Code("tool_executor_missing").
			Errorf("tool executor is not configured")
	}
	results, err := request.ExecuteTools(ctx, toolCallsToLLM(calls), request.OnEvent)
	if err != nil {
		return nil, nil, oops.In("provider").
			Code("tool_execution_failed").
			Wrapf(err, "execute tool calls")
	}
	events := toolEventsFromLLM(results)
	outputs := make([]any, 0, len(calls))
	for index, call := range calls {
		var event *ToolEvent
		if index < len(events) {
			event = &events[index]
		}
		outputs = append(outputs, toolOutputForCall(call.ID, event))
	}

	return outputs, events, nil
}

func toolCallsToLLM(calls []ToolCall) []llm.ToolCall {
	if len(calls) == 0 {
		return nil
	}

	results := make([]llm.ToolCall, 0, len(calls))
	for _, call := range calls {
		results = append(results, llm.ToolCall{
			Metadata:      toolCallMetadata(call),
			Arguments:     cloneAnyMap(call.Arguments),
			ID:            call.ID,
			Name:          call.Name,
			ArgumentsJSON: call.ArgumentsJSON,
		})
	}

	return results
}

func toolCallMetadata(call ToolCall) map[string]any {
	metadata := cloneAnyMap(call.Metadata)
	if !call.TextFallback {
		return metadata
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["text_fallback"] = true

	return metadata
}

func toolEventsFromLLM(results []llm.ToolResult) []ToolEvent {
	if len(results) == 0 {
		return nil
	}

	events := make([]ToolEvent, 0, len(results))
	for index := range results {
		events = append(events, toolEventFromLLM(&results[index]))
	}

	return events
}

func toolEventFromLLM(result *llm.ToolResult) ToolEvent {
	if result == nil {
		return ToolEvent{Name: "", ArgumentsJSON: "", DetailsJSON: "", Result: "", Error: "", IsError: false}
	}

	return ToolEvent{
		Name:          result.Name,
		ArgumentsJSON: result.ArgumentsJSON,
		DetailsJSON:   stringFromOptions(result.Metadata, "details_json"),
		Result:        partsText(result.Content),
		Error:         result.Error,
		IsError:       result.IsError,
	}
}

func toolResultPartFromEvent(event *ToolEvent) llm.Part {
	return llm.Part{
		Metadata:   nil,
		ToolCall:   nil,
		ToolResult: toolResultFromEvent(event),
		Type:       llm.PartToolResult,
		Text:       "",
		Data:       "",
		MIMEType:   "",
	}
}

func toolResultFromEvent(event *ToolEvent) *llm.ToolResult {
	if event == nil {
		return &llm.ToolResult{
			Metadata:      nil,
			ToolCallID:    "",
			ArgumentsJSON: "",
			Name:          "",
			Error:         "",
			Content:       nil,
			IsError:       false,
		}
	}

	return &llm.ToolResult{
		Metadata:      toolResultMetadataFromEvent(event),
		ToolCallID:    "",
		ArgumentsJSON: event.ArgumentsJSON,
		Name:          event.Name,
		Error:         event.Error,
		Content:       []llm.Part{llm.TextPart(event.Result)},
		IsError:       event.IsError,
	}
}

func toolResultMetadataFromEvent(event *ToolEvent) map[string]any {
	if event == nil || strings.TrimSpace(event.DetailsJSON) == "" {
		return nil
	}

	return map[string]any{"details_json": event.DetailsJSON}
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

func finishProviderResult(result *llm.Response, providerResult *providerResult) (bool, error) {
	if providerResult == nil {
		result.FinishReason = llm.FinishReasonStop
		return true, nil
	}
	result.FinishReason = providerResult.FinishReason
	if result.FinishReason == llm.FinishReasonUnknown {
		result.FinishReason = llm.FinishReasonStop
	}
	if text := strings.TrimSpace(providerResult.Text); text != "" {
		result.Content = append(result.Content, llm.TextPart(text))
	}

	return true, nil
}

func emitStreamEvent(onEvent func(*llm.StreamChunk), event StreamEvent) {
	if onEvent != nil {
		onEvent(streamChunkToLLM(event))
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

func stringFromOptions(options map[string]any, key string) string {
	value, ok := options[key].(string)
	if !ok {
		return ""
	}

	return value
}
