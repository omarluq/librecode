package assistant

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/model"
)

type sseAccumulator struct {
	itemByID      map[string]map[string]any
	finalResponse map[string]any
	parts         []string
	items         []any
}

func newSSEAccumulator() *sseAccumulator {
	return &sseAccumulator{
		itemByID:      map[string]map[string]any{},
		finalResponse: nil,
		parts:         []string{},
		items:         []any{},
	}
}

func (accumulator *sseAccumulator) add(event map[string]any, onEvent func(StreamEvent)) {
	accumulator.addResponse(event)
	accumulator.addUsage(event)
	if text, delta := thinkingTextFromSSEEvent(event); delta && text != "" {
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: nil,
			Usage:     nil,
			Kind:      StreamEventThinkingDelta,
			Text:      text,
		})
	}
	if text, delta := textFromSSEEvent(event); delta && text != "" {
		accumulator.parts = append(accumulator.parts, text)
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: nil,
			Usage:     nil,
			Kind:      StreamEventTextDelta,
			Text:      text,
		})
	}
	if item, ok := event["item"].(map[string]any); ok {
		accumulator.addItem(item)
	}
	if arguments, ok := event["arguments"].(string); ok {
		accumulator.addArguments(event, arguments)
	}
}

func (accumulator *sseAccumulator) addResponse(event map[string]any) {
	response, ok := event["response"].(map[string]any)
	if !ok {
		return
	}
	if accumulator.finalResponse != nil {
		if usage := accumulator.finalResponse["usage"]; usage != nil && response["usage"] == nil {
			response["usage"] = usage
		}
	}
	accumulator.finalResponse = response
}

func (accumulator *sseAccumulator) addUsage(event map[string]any) {
	usage, ok := event["usage"].(map[string]any)
	if !ok {
		return
	}
	accumulator.finalResponse = ensureSSEFinalResponse(accumulator.finalResponse)
	accumulator.finalResponse["usage"] = usage
}

func (accumulator *sseAccumulator) addItem(item map[string]any) {
	itemID := stringValue(item["id"])
	if itemID != "" {
		accumulator.itemByID[itemID] = item
	}
	accumulator.items = upsertSSEItem(accumulator.items, item)
}

func (accumulator *sseAccumulator) addArguments(event map[string]any, arguments string) {
	itemID := sseItemID(event)
	if itemID == "" {
		return
	}
	item, ok := accumulator.itemByID[itemID]
	if !ok {
		item = map[string]any{
			"id":        itemID,
			jsonTypeKey: functionCallType,
		}
		accumulator.itemByID[itemID] = item
	}
	item["arguments"] = arguments
	accumulator.items = upsertSSEItem(accumulator.items, item)
}

func ensureSSEFinalResponse(response map[string]any) map[string]any {
	if response != nil {
		return response
	}

	return map[string]any{}
}

func sseItemID(event map[string]any) string {
	for _, key := range []string{"item_id", "output_item_id", "id"} {
		if value := stringValue(event[key]); value != "" {
			return value
		}
	}
	if item, ok := event["item"].(map[string]any); ok {
		return stringValue(item["id"])
	}

	return ""
}

func upsertSSEItem(items []any, item map[string]any) []any {
	itemID := stringValue(item["id"])
	if itemID == "" {
		return append(items, item)
	}
	for index, existing := range items {
		existingItem, ok := existing.(map[string]any)
		if ok && stringValue(existingItem["id"]) == itemID {
			items[index] = item
			return items
		}
	}

	return append(items, item)
}

func parseSSEResult(reader io.Reader, onEvent func(StreamEvent)) (*providerResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	accumulator, err := scanSSEResponse(scanner, onEvent)
	if err != nil {
		return nil, err
	}
	fallbackText := strings.TrimSpace(strings.Join(accumulator.parts, ""))
	if accumulator.finalResponse != nil {
		result := providerResultFromResponse(accumulator.finalResponse)
		if len(result.OutputItems) == 0 && len(accumulator.items) > 0 {
			usage := result.Usage
			result = providerResultFromOutputItems(accumulator.items, fallbackText)
			result.Usage = usage
		}
		if strings.TrimSpace(result.Text) == "" {
			result.Text = fallbackText
		}

		return result, nil
	}
	if len(accumulator.items) > 0 {
		return providerResultFromOutputItems(accumulator.items, fallbackText), nil
	}

	return &providerResult{
		Text:        fallbackText,
		OutputItems: nil,
		Thinking:    nil,
		ToolCalls:   nil,
		Usage:       model.EmptyTokenUsage(),
	}, nil
}

func scanSSEResponse(scanner *bufio.Scanner, onEvent func(StreamEvent)) (accumulator *sseAccumulator, err error) {
	accumulator = newSSEAccumulator()
	for scanner.Scan() {
		event, ok := eventFromSSELine(scanner.Text())
		if ok {
			accumulator.add(event, onEvent)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, oops.In("assistant").Code("sse_read").Wrapf(err, "read provider stream")
	}

	return accumulator, nil
}

func eventFromSSELine(line string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return nil, false
	}
	data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if data == "" || data == "[DONE]" {
		return nil, false
	}

	return decodeEvent([]byte(data))
}

func decodeEvent(data []byte) (map[string]any, bool) {
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, false
	}

	return event, true
}

func thinkingTextFromSSEEvent(event map[string]any) (text string, delta bool) {
	eventType := ""
	if value, ok := event[jsonTypeKey].(string); ok {
		eventType = value
	}
	if !isThinkingDeltaEvent(eventType) {
		return "", false
	}

	return deltaTextFromSSEEvent(event)
}

func textFromSSEEvent(event map[string]any) (text string, delta bool) {
	eventType := ""
	if value, ok := event[jsonTypeKey].(string); ok {
		eventType = value
	}
	if !isTextDeltaEvent(eventType) {
		return "", false
	}

	return deltaTextFromSSEEvent(event)
}

func deltaTextFromSSEEvent(event map[string]any) (text string, delta bool) {
	if deltaText, ok := event["delta"].(string); ok {
		return deltaText, true
	}
	if eventText, ok := event["text"].(string); ok {
		return eventText, true
	}

	return "", false
}

func isTextDeltaEvent(eventType string) bool {
	if isThinkingDeltaEvent(eventType) {
		return false
	}

	return strings.Contains(eventType, "output_text.delta") ||
		strings.Contains(eventType, "text.delta") ||
		strings.Contains(eventType, "content_part.delta")
}

func isThinkingDeltaEvent(eventType string) bool {
	return strings.Contains(eventType, "reasoning") && strings.Contains(eventType, "text.delta")
}
