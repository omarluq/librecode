package provider

import (
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
)

const (
	anthropicMessageStartEvent      = "message_start"
	anthropicContentBlockStartEvent = "content_block_start"
	anthropicContentBlockDeltaEvent = "content_block_delta"
	anthropicMessageDeltaEvent      = "message_delta"
	anthropicMessageStopEvent       = "message_stop"
	anthropicPingEvent              = "ping"
	anthropicTextDelta              = "text_delta"
	anthropicThinkingDelta          = "thinking_delta"
	anthropicInputJSONDelta         = "input_json_delta"
	anthropicSignatureDelta         = "signature_delta"
	anthropicThinkingBlock          = "thinking"
	anthropicRedactedThinkingBlock  = "redacted_thinking"
)

type anthropicStreamEvent struct {
	Error        providerError               `json:"error"`
	Message      anthropicStreamMessage      `json:"message"`
	Delta        anthropicStreamDelta        `json:"delta"`
	ContentBlock anthropicStreamContentBlock `json:"content_block"`
	Usage        map[string]any              `json:"usage"`
	Type         string                      `json:"type"`
	Index        int                         `json:"index"`
}

type anthropicStreamMessage struct {
	Usage       map[string]any        `json:"usage"`
	StopDetails *anthropicStopDetails `json:"stop_details"`
	StopReason  string                `json:"stop_reason"`
}

type anthropicStreamDelta struct {
	StopDetails *anthropicStopDetails `json:"stop_details"`
	PartialJSON string                `json:"partial_json"`
	Signature   string                `json:"signature"`
	Thinking    string                `json:"thinking"`
	Text        string                `json:"text"`
	StopReason  string                `json:"stop_reason"`
	Type        string                `json:"type"`
}

type anthropicStreamContentBlock struct {
	Input any    `json:"input"`
	Text  string `json:"text"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
}

type anthropicStreamAccumulator struct {
	toolBlocks  map[int]*anthropicStreamToolBlock
	textParts   []string
	thinking    []string
	stopDetails *anthropicStopDetails
	stopReason  string
	usage       llm.Usage
	terminal    bool
}

type anthropicStreamToolBlock struct {
	Arguments string
	Initial   any
	ID        string
	Name      string
	Index     int
}

func parseAnthropicStream(reader io.Reader, onEvent func(*llm.StreamChunk)) (*providerResult, error) {
	accumulator := &anthropicStreamAccumulator{
		toolBlocks:  map[int]*anthropicStreamToolBlock{},
		textParts:   []string{},
		thinking:    []string{},
		stopDetails: nil,
		stopReason:  "",
		usage:       llm.EmptyUsage(),
		terminal:    false,
	}

	err := scanSSEEvents(reader, func(event sseEvent) error {
		eventType := firstNonEmptyString(event.Name, eventTypeFromData(event.Data))
		if eventType == anthropicPingEvent || strings.TrimSpace(event.Data) == "" {
			return nil
		}

		var decoded anthropicStreamEvent
		if err := decodeSSEJSON(event.Data, &decoded, "anthropic_stream_decode"); err != nil {
			return err
		}

		if decoded.Type == "" {
			decoded.Type = eventType
		}

		return accumulator.add(&decoded, onEvent)
	})
	if err != nil {
		return nil, err
	}

	if !accumulator.terminal {
		return nil, oops.In("provider").
			Code("anthropic_stream_incomplete").
			Errorf("provider stream closed before completion")
	}

	return accumulator.result(), nil
}

func eventTypeFromData(data string) string {
	var value struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		return ""
	}

	return value.Type
}

func (accumulator *anthropicStreamAccumulator) add(
	event *anthropicStreamEvent,
	onEvent func(*llm.StreamChunk),
) error {
	switch event.Type {
	case anthropicMessageStartEvent:
		accumulator.usage = mergeUsage(accumulator.usage, usageFromObject(event.Message.Usage))
	case anthropicContentBlockStartEvent:
		accumulator.startBlock(event)
	case anthropicContentBlockDeltaEvent:
		accumulator.addDelta(event, onEvent)
	case anthropicMessageDeltaEvent:
		accumulator.addMessageDelta(event)
	case anthropicMessageStopEvent:
		accumulator.terminal = true
	case anthropicErrorEvent:
		return providerErrorToOops("anthropic_error", &event.Error)
	}

	return nil
}

func (accumulator *anthropicStreamAccumulator) addMessageDelta(event *anthropicStreamEvent) {
	if event.Delta.StopReason != "" {
		accumulator.stopReason = event.Delta.StopReason
	}

	if event.Delta.StopDetails != nil {
		accumulator.stopDetails = event.Delta.StopDetails
	}

	accumulator.usage = mergeUsage(accumulator.usage, usageFromObject(event.Usage))
}

func (accumulator *anthropicStreamAccumulator) startBlock(event *anthropicStreamEvent) {
	block := event.ContentBlock
	switch block.Type {
	case jsonTextKey:
		if block.Text != "" {
			accumulator.textParts = append(accumulator.textParts, block.Text)
		}
	case anthropicThinkingBlock, anthropicRedactedThinkingBlock:
		if block.Text != "" {
			accumulator.thinking = append(accumulator.thinking, block.Text)
		}
	case anthropicToolUseType:
		tool := accumulator.toolBlock(event.Index)
		tool.ID = block.ID
		tool.Name = block.Name
		tool.Initial = block.Input
	}
}

func (accumulator *anthropicStreamAccumulator) addDelta(
	event *anthropicStreamEvent,
	onEvent func(*llm.StreamChunk),
) {
	switch event.Delta.Type {
	case anthropicTextDelta:
		accumulator.addTextDelta(event.Delta.Text, onEvent)
	case anthropicThinkingDelta:
		accumulator.addThinkingDelta(event.Delta.Thinking, onEvent)
	case anthropicInputJSONDelta:
		if event.Delta.PartialJSON != "" {
			block := accumulator.toolBlock(event.Index)
			block.Arguments += event.Delta.PartialJSON
		}
	case anthropicSignatureDelta:
		// Signatures are validated by Anthropic. Keep unknown metadata out of the
		// completed provider result until response parts carry provider metadata.
	}
}

func (accumulator *anthropicStreamAccumulator) addTextDelta(text string, onEvent func(*llm.StreamChunk)) {
	if text == "" {
		return
	}

	accumulator.textParts = append(accumulator.textParts, text)
	emitStreamEvent(onEvent, StreamEvent{
		ToolEvent: nil,
		Usage:     nil,
		Kind:      StreamEventTextDelta,
		Text:      text,
	})
}

func (accumulator *anthropicStreamAccumulator) addThinkingDelta(text string, onEvent func(*llm.StreamChunk)) {
	if text == "" {
		return
	}

	accumulator.thinking = append(accumulator.thinking, text)
	emitStreamEvent(onEvent, StreamEvent{
		ToolEvent: nil,
		Usage:     nil,
		Kind:      StreamEventThinkingDelta,
		Text:      text,
	})
}

func (accumulator *anthropicStreamAccumulator) toolBlock(index int) *anthropicStreamToolBlock {
	block, ok := accumulator.toolBlocks[index]
	if ok {
		return block
	}

	block = &anthropicStreamToolBlock{
		Arguments: "",
		Initial:   nil,
		ID:        "",
		Name:      "",
		Index:     index,
	}
	accumulator.toolBlocks[index] = block

	return block
}

func (accumulator *anthropicStreamAccumulator) result() *providerResult {
	calls := accumulator.toolCalls()
	finishReason := anthropicFinishReason(accumulator.stopReason, len(calls) > 0)

	text := strings.TrimSpace(strings.Join(accumulator.textParts, ""))
	if finishReason == llm.FinishReasonRefusal {
		if text == "" {
			text = anthropicRefusalText(accumulator.stopDetails)
		}

		calls = nil
	}

	return &providerResult{
		FinishReason: finishReason,
		Text:         text,
		OutputItems:  nil,
		Thinking:     accumulator.thinking,
		ToolCalls:    calls,
		Usage:        accumulator.usage,
	}
}

func (accumulator *anthropicStreamAccumulator) toolCalls() []ToolCall {
	indexes := make([]int, 0, len(accumulator.toolBlocks))
	for index := range accumulator.toolBlocks {
		indexes = append(indexes, index)
	}

	sort.Ints(indexes)

	calls := make([]ToolCall, 0, len(indexes))
	for _, index := range indexes {
		block := accumulator.toolBlocks[index]
		input := block.Initial

		if argumentsJSON := strings.TrimSpace(block.Arguments); argumentsJSON != "" {
			var decoded any
			if err := json.Unmarshal([]byte(argumentsJSON), &decoded); err == nil {
				input = decoded
			} else {
				input = argumentsJSON
			}
		}

		calls = append(calls, anthropicToolCall(block.ID, block.Name, input))
	}

	return calls
}
