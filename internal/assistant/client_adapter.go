package assistant

import (
	"context"
	"strings"

	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/llmconv"
	"github.com/omarluq/librecode/internal/mapsutil"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/provider"
)

func providerRequestFromCompletionRequest(request *CompletionRequest) *provider.CompletionRequest {
	if request == nil {
		return emptyProviderRequest()
	}

	return &provider.CompletionRequest{
		OnProviderObserve: llmProviderObserver(request.OnProviderObserve),
		OnProviderRequest: request.OnProviderRequest,
		ExecuteTools:      llmToolExecutor(request.ExecuteTools),
		OnEvent:           llmStreamEventHandler(request.OnEvent),
		Request:           llmRequestFromCompletionRequest(request),
		ProviderAttempt:   request.ProviderAttempt,
	}
}

func emptyProviderRequest() *provider.CompletionRequest {
	return &provider.CompletionRequest{
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ExecuteTools:      nil,
		OnEvent:           nil,
		Request:           emptyLLMRequest(),
		ProviderAttempt:   0,
	}
}

func llmProviderObserver(observer func(context.Context, *CompletionRequest, int)) llm.ProviderObserver {
	if observer == nil {
		return nil
	}

	return func(ctx context.Context, input *llm.HookInput) {
		observer(ctx, completionRequestFromHookInput(input), hookAttempt(input))
	}
}

func completionRequestFromHookInput(input *llm.HookInput) *CompletionRequest {
	if input == nil {
		return nil
	}

	return &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ToolRegistry:      nil,
		ExecuteTools:      nil,
		SessionID:         input.SessionID,
		SystemPrompt:      "",
		ThinkingLevel:     input.ThinkingLevel,
		CWD:               stringFromOptions(input.ProviderOptions, "cwd"),
		Auth:              requestAuthFromHookInput(input),
		Messages:          nil,
		Usage:             model.EmptyTokenUsage(),
		Model:             modelFromLLMRef(&input.Model),
		ProviderAttempt:   input.Attempt,
		DisableTools:      false,
	}
}

func requestAuthFromHookInput(input *llm.HookInput) model.RequestAuth {
	return model.RequestAuth{
		Headers: mapsutil.CloneOrNil(input.Headers),
		APIKey:  "",
		Error:   "",
		OK:      false,
	}
}

func modelFromLLMRef(input *llm.ModelRef) model.Model {
	if input == nil {
		return model.Model{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           nil,
			Provider:         "",
			ID:               "",
			Name:             "",
			API:              "",
			BaseURL:          "",
			Input:            nil,
			Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    0,
			MaxTokens:        0,
			Reasoning:        false,
		}
	}

	return model.Model{
		ThinkingLevelMap: thinkingLevelMapFromLLM(input.ThinkingLevelMap),
		Headers:          nil,
		Compat:           mapsutil.CloneOrNil(input.Metadata),
		Provider:         input.Provider,
		ID:               input.ID,
		Name:             input.ID,
		API:              input.API,
		BaseURL:          input.BaseURL,
		Input:            nil,
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    input.ContextWindow,
		MaxTokens:        input.MaxTokens,
		Reasoning:        input.Reasoning,
	}
}

func thinkingLevelMapFromLLM(values map[string]*string) map[model.ThinkingLevel]*string {
	if values == nil {
		return nil
	}
	converted := make(map[model.ThinkingLevel]*string, len(values))
	for level, value := range values {
		converted[model.ThinkingLevel(level)] = value
	}

	return converted
}

func completionResultFromLLMResponse(response *llm.Response) *CompletionResult {
	if response == nil {
		return &CompletionResult{
			FinishReason: llm.FinishReasonUnknown,
			Text:         "",
			Thinking:     nil,
			ToolEvents:   nil,
			Usage:        model.EmptyTokenUsage(),
		}
	}
	return &CompletionResult{
		FinishReason: response.FinishReason,
		Text:         textFromLLMParts(response.Content),
		Thinking:     thinkingFromLLMParts(response.Content),
		ToolEvents:   toolEventsFromLLMParts(response.Content),
		Usage:        llmconv.UsageToModel(response.Usage),
	}
}

func textFromLLMParts(parts []llm.Part) string {
	texts := lo.FilterMap(parts, func(part llm.Part, _ int) (string, bool) {
		return part.Text, part.Type == llm.PartText && strings.TrimSpace(part.Text) != ""
	})

	return strings.TrimSpace(strings.Join(texts, "\n"))
}

func thinkingFromLLMParts(parts []llm.Part) []string {
	thinking := lo.FilterMap(parts, func(part llm.Part, _ int) (string, bool) {
		return part.Text, part.Type == llm.PartReasoning && strings.TrimSpace(part.Text) != ""
	})
	if len(thinking) == 0 {
		return nil
	}

	return thinking
}

func toolEventsFromLLMParts(parts []llm.Part) []ToolEvent {
	events := lo.FilterMap(parts, func(part llm.Part, _ int) (ToolEvent, bool) {
		if part.ToolResult == nil {
			return emptyToolEvent(), false
		}

		return toolEventFromLLMToolResult(part.ToolResult), true
	})
	if len(events) == 0 {
		return nil
	}

	return events
}

func emptyToolEvent() ToolEvent {
	return ToolEvent{Name: "", ArgumentsJSON: "", DetailsJSON: "", Result: "", Error: "", IsError: false}
}

func toolEventFromLLMToolResult(result *llm.ToolResult) ToolEvent {
	if result == nil {
		return emptyToolEvent()
	}

	return ToolEvent{
		Name:          result.Name,
		ArgumentsJSON: result.ArgumentsJSON,
		DetailsJSON:   stringFromOptions(result.Metadata, "details_json"),
		Result:        textFromLLMParts(result.Content),
		Error:         result.Error,
		IsError:       result.IsError,
	}
}

func llmStreamEventHandler(onEvent func(StreamEvent)) func(*llm.StreamChunk) {
	if onEvent == nil {
		return nil
	}

	return func(chunk *llm.StreamChunk) {
		onEvent(streamEventFromLLMChunk(chunk))
	}
}

func streamEventFromLLMChunk(chunk *llm.StreamChunk) StreamEvent {
	return StreamEvent{
		ToolEvent: toolEventPointerFromLLMPart(chunkPart(chunk)),
		Usage:     usagePointerFromLLMUsage(chunkUsage(chunk)),
		Kind:      streamEventKindFromLLMChunk(chunk),
		Text:      textFromLLMChunk(chunk),
	}
}

func streamEventKindFromLLMChunk(chunk *llm.StreamChunk) StreamEventKind {
	part := chunkPart(chunk)
	if part == nil {
		return StreamEventTextDelta
	}
	switch part.Type {
	case llm.PartReasoning:
		return StreamEventThinkingDelta
	case llm.PartToolResult:
		return StreamEventToolResult
	case llm.PartToolCall:
		return StreamEventToolStart
	case llm.PartText,
		llm.PartImage,
		llm.PartFile,
		llm.PartSource:
		return StreamEventTextDelta
	}

	return StreamEventTextDelta
}

func textFromLLMChunk(chunk *llm.StreamChunk) string {
	part := chunkPart(chunk)
	if part == nil {
		return ""
	}
	if part.Type == llm.PartToolCall && part.ToolCall != nil {
		return part.ToolCall.Name
	}

	return part.Text
}

func chunkPart(chunk *llm.StreamChunk) *llm.Part {
	if chunk == nil {
		return nil
	}

	return chunk.Part
}

func chunkUsage(chunk *llm.StreamChunk) llm.Usage {
	if chunk == nil {
		return llm.EmptyUsage()
	}

	return chunk.Usage
}

func toolEventPointerFromLLMPart(part *llm.Part) *ToolEvent {
	if part == nil || part.ToolResult == nil {
		return nil
	}
	event := toolEventFromLLMToolResult(part.ToolResult)

	return &event
}

func usagePointerFromLLMUsage(usage llm.Usage) *model.TokenUsage {
	if !usage.HasAny() {
		return nil
	}
	converted := llmconv.UsageToModel(usage)

	return &converted
}

func llmToolExecutor(executor ToolExecutor) llm.ToolExecutor {
	if executor == nil {
		return nil
	}

	return func(ctx context.Context, calls []llm.ToolCall, onEvent func(*llm.StreamChunk)) ([]llm.ToolResult, error) {
		events, err := executor(ctx, assistantToolCallsFromLLM(calls), assistantStreamEventHandler(onEvent))
		if err != nil {
			return nil, err
		}

		return lo.Map(events, func(event ToolEvent, _ int) llm.ToolResult {
			return llmToolResultFromToolEvent(&event)
		}), nil
	}
}

func assistantToolCallsFromLLM(calls []llm.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}

	return lo.Map(calls, func(call llm.ToolCall, _ int) ToolCall {
		return ToolCall{
			Metadata:      mapsutil.CloneOrNil(call.Metadata),
			Arguments:     mapsutil.CloneOrNil(call.Arguments),
			ID:            call.ID,
			Name:          call.Name,
			ArgumentsJSON: call.ArgumentsJSON,
		}
	})
}

func assistantStreamEventHandler(onEvent func(*llm.StreamChunk)) func(StreamEvent) {
	if onEvent == nil {
		return nil
	}

	return func(event StreamEvent) {
		onEvent(llmStreamChunkFromEvent(event))
	}
}

func llmStreamChunkFromEvent(event StreamEvent) *llm.StreamChunk {
	return &llm.StreamChunk{
		Part:         llmPartFromStreamEvent(event),
		ToolCall:     nil,
		FinishReason: llm.FinishReasonUnknown,
		Usage:        llmUsageFromPointer(event.Usage),
	}
}

func llmPartFromStreamEvent(event StreamEvent) *llm.Part {
	switch event.Kind {
	case StreamEventThinkingDelta:
		part := llm.Part{
			Metadata:   nil,
			ToolCall:   nil,
			ToolResult: nil,
			Type:       llm.PartReasoning,
			Text:       event.Text,
			Data:       "",
			MIMEType:   "",
		}
		return &part
	case StreamEventToolStart:
		call := llm.ToolCall{
			Metadata:      nil,
			Arguments:     nil,
			ID:            "",
			Name:          event.Text,
			ArgumentsJSON: "",
		}
		part := llm.Part{
			Metadata:   nil,
			ToolCall:   &call,
			ToolResult: nil,
			Type:       llm.PartToolCall,
			Text:       "",
			Data:       "",
			MIMEType:   "",
		}
		return &part
	case StreamEventToolResult:
		if event.ToolEvent == nil {
			return nil
		}
		result := llmToolResultFromToolEvent(event.ToolEvent)
		part := llm.Part{
			Metadata:   nil,
			ToolCall:   nil,
			ToolResult: &result,
			Type:       llm.PartToolResult,
			Text:       "",
			Data:       "",
			MIMEType:   "",
		}
		return &part
	case StreamEventTextDelta,
		StreamEventSkillLoaded,
		StreamEventUsage,
		StreamEventUsageSnapshot,
		StreamEventContextCompaction,
		StreamEventContextCompactionStart,
		StreamEventContextCompactionDone,
		StreamEventContextCompactionError,
		StreamEventUnknown:
		part := llm.TextPart(event.Text)
		return &part
	}

	part := llm.TextPart(event.Text)
	return &part
}

func llmUsageFromPointer(usage *model.TokenUsage) llm.Usage {
	if usage == nil {
		return llm.EmptyUsage()
	}

	return llmconv.UsageFromModel(*usage)
}

func stringFromOptions(options map[string]any, key string) string {
	value, ok := options[key].(string)
	if !ok {
		return ""
	}

	return value
}

func hookAttempt(input *llm.HookInput) int {
	if input == nil {
		return 0
	}

	return input.Attempt
}
