package provider

import (
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
)

type openAIChatStreamChunk struct {
	Error   providerError            `json:"error"`
	Usage   map[string]any           `json:"usage"`
	Choices []openAIChatStreamChoice `json:"choices"`
}

type openAIChatStreamChoice struct {
	Delta        *openAIChatStreamDelta `json:"delta"`
	Usage        map[string]any         `json:"usage"`
	FinishReason string                 `json:"finish_reason"`
}

type openAIChatStreamDelta struct {
	Content          string                          `json:"content"`
	ReasoningContent string                          `json:"reasoning_content"`
	Reasoning        string                          `json:"reasoning"`
	ReasoningText    string                          `json:"reasoning_text"`
	ToolCalls        []openAIChatStreamToolCallDelta `json:"tool_calls"`
}

type openAIChatStreamToolCallDelta struct {
	Function openAIChatStreamFunctionDelta `json:"function"`
	ID       string                        `json:"id"`
	Type     string                        `json:"type"`
	Index    int                           `json:"index"`
}

type openAIChatStreamFunctionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatStreamAccumulator struct {
	toolCalls    map[int]*openAIChatStreamToolCall
	textParts    []string
	thinking     []string
	finishReason string
	usage        llm.Usage
	terminal     bool
}

type openAIChatStreamToolCall struct {
	Arguments string
	ID        string
	Name      string
	Kind      string
	Index     int
}

func parseOpenAIChatStream(reader io.Reader, onEvent func(*llm.StreamChunk)) (*providerResult, error) {
	accumulator := &openAIChatStreamAccumulator{
		toolCalls:    map[int]*openAIChatStreamToolCall{},
		textParts:    []string{},
		thinking:     []string{},
		finishReason: "",
		usage:        llm.EmptyUsage(),
		terminal:     false,
	}

	err := scanSSEEvents(reader, func(event sseEvent) error {
		trimmed := strings.TrimSpace(event.Data)
		if trimmed == "" {
			return nil
		}

		if trimmed == sseDoneData {
			accumulator.terminal = true

			return errSSEDone
		}

		var chunk openAIChatStreamChunk
		if err := decodeSSEJSON(event.Data, &chunk, "openai_chat_stream_decode"); err != nil {
			return err
		}

		return accumulator.add(&chunk, onEvent)
	})
	if err != nil && !errors.Is(err, errSSEDone) {
		return nil, err
	}

	result := accumulator.result()
	if !accumulator.completeWithContent(result) {
		return nil, oops.In("provider").
			Code("openai_chat_stream_incomplete").
			Errorf("provider stream closed before completion")
	}

	return result, nil
}

func (accumulator *openAIChatStreamAccumulator) completeWithContent(result *providerResult) bool {
	if !accumulator.terminal && accumulator.finishReason == "" {
		return false
	}

	return strings.TrimSpace(result.Text) != "" ||
		len(result.Thinking) > 0 ||
		len(result.ToolCalls) > 0 ||
		result.FinishReason != llm.FinishReasonUnknown
}

func (accumulator *openAIChatStreamAccumulator) add(
	chunk *openAIChatStreamChunk,
	onEvent func(*llm.StreamChunk),
) error {
	if chunk.Error.Message != "" {
		return providerErrorToOops("openai_chat_error", &chunk.Error)
	}

	if len(chunk.Usage) > 0 {
		accumulator.usage = mergeUsage(accumulator.usage, usageFromObject(chunk.Usage))
	}

	for _, choice := range chunk.Choices {
		if len(choice.Usage) > 0 {
			accumulator.usage = mergeUsage(accumulator.usage, usageFromObject(choice.Usage))
		}

		accumulator.addDelta(choice.Delta, onEvent)

		if choice.FinishReason != "" {
			accumulator.finishReason = choice.FinishReason
			accumulator.terminal = true
		}
	}

	return nil
}

func (accumulator *openAIChatStreamAccumulator) addDelta(
	delta *openAIChatStreamDelta,
	onEvent func(*llm.StreamChunk),
) {
	if delta == nil {
		return
	}

	if delta.Content != "" {
		accumulator.textParts = append(accumulator.textParts, delta.Content)
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: nil,
			Usage:     nil,
			Kind:      StreamEventTextDelta,
			Text:      delta.Content,
		})
	}

	if reasoning := openAIChatReasoningDelta(delta); reasoning != "" {
		accumulator.thinking = append(accumulator.thinking, reasoning)
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: nil,
			Usage:     nil,
			Kind:      StreamEventThinkingDelta,
			Text:      reasoning,
		})
	}

	for _, deltaCall := range delta.ToolCalls {
		call := accumulator.toolCall(deltaCall.Index)
		if deltaCall.ID != "" {
			call.ID = deltaCall.ID
		}

		if deltaCall.Type != "" {
			call.Kind = deltaCall.Type
		}

		if deltaCall.Function.Name != "" {
			call.Name = deltaCall.Function.Name
		}

		if deltaCall.Function.Arguments != "" {
			call.Arguments += deltaCall.Function.Arguments
		}
	}
}

func openAIChatReasoningDelta(delta *openAIChatStreamDelta) string {
	for _, value := range []string{delta.ReasoningContent, delta.Reasoning, delta.ReasoningText} {
		if value != "" {
			return value
		}
	}

	return ""
}

func (accumulator *openAIChatStreamAccumulator) toolCall(index int) *openAIChatStreamToolCall {
	call, ok := accumulator.toolCalls[index]
	if ok {
		return call
	}

	call = &openAIChatStreamToolCall{
		Arguments: "",
		ID:        "",
		Name:      "",
		Kind:      "",
		Index:     index,
	}
	accumulator.toolCalls[index] = call

	return call
}

func (accumulator *openAIChatStreamAccumulator) result() *providerResult {
	calls := accumulator.completedToolCalls()

	return &providerResult{
		FinishReason: openAIChatFinishReason(accumulator.finishReason, len(calls) > 0),
		Text:         strings.TrimSpace(strings.Join(accumulator.textParts, "")),
		OutputItems:  nil,
		Thinking:     accumulator.thinking,
		ToolCalls:    calls,
		Usage:        accumulator.usage,
	}
}

func (accumulator *openAIChatStreamAccumulator) completedToolCalls() []ToolCall {
	indexes := make([]int, 0, len(accumulator.toolCalls))
	for index := range accumulator.toolCalls {
		indexes = append(indexes, index)
	}

	sort.Ints(indexes)

	calls := make([]ToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := accumulator.toolCalls[index]
		if call.Kind != "" && call.Kind != functionToolType {
			continue
		}

		argumentsJSON := call.Arguments
		calls = append(calls, ToolCall{
			Arguments:     toolArgumentsFromJSON(argumentsJSON),
			Metadata:      nil,
			ID:            call.ID,
			Name:          call.Name,
			ArgumentsJSON: argumentsJSON,
			TextFallback:  false,
		})
	}

	return calls
}
