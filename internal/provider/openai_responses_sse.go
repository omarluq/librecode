package provider

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
)

type sseAccumulator struct {
	itemByID              map[string]map[string]any
	finalResponse         map[string]any
	terminalErr           error
	parts                 []string
	thinkingParts         []string
	items                 []any
	completed             bool
	terminal              bool
	sawTypedResponseEvent bool
}

func newSSEAccumulator() *sseAccumulator {
	return &sseAccumulator{
		itemByID:              map[string]map[string]any{},
		finalResponse:         nil,
		terminalErr:           nil,
		parts:                 []string{},
		thinkingParts:         []string{},
		items:                 []any{},
		completed:             false,
		terminal:              false,
		sawTypedResponseEvent: false,
	}
}

func (accumulator *sseAccumulator) add(event map[string]any, onEvent func(*llm.StreamChunk)) error {
	accumulator.addResponseEventState(event)

	if accumulator.terminalErr != nil {
		return errSSEDone
	}

	accumulator.addResponse(event)
	accumulator.addUsage(event)

	if text, delta := thinkingTextFromSSEEvent(event); delta && text != "" {
		accumulator.thinkingParts = append(accumulator.thinkingParts, text)
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

	if accumulator.terminal {
		return errSSEDone
	}

	return nil
}

func (accumulator *sseAccumulator) addResponseEventState(event map[string]any) {
	eventType := stringValue(event[jsonTypeKey])
	if !strings.HasPrefix(eventType, "response.") {
		return
	}

	accumulator.sawTypedResponseEvent = true

	switch eventType {
	case "response.completed", "response.done":
		accumulator.completed = true
		accumulator.terminal = true
	case "response.failed":
		accumulator.terminal = true
		accumulator.terminalErr = sseProviderError("responses_failed", event, "provider response failed")
	case "response.incomplete":
		accumulator.terminal = true
	}
}

func (accumulator *sseAccumulator) addResponse(event map[string]any) {
	response, ok := event["response"].(map[string]any)
	if !ok {
		return
	}

	if accumulator.completed && !responseHasResultData(response) && accumulator.finalResponse != nil {
		if accumulator.finalResponse["usage"] == nil && response["usage"] != nil {
			accumulator.finalResponse["usage"] = response["usage"]
		}

		return
	}

	if accumulator.finalResponse != nil {
		if usage := accumulator.finalResponse["usage"]; usage != nil && response["usage"] == nil {
			response["usage"] = usage
		}
	}

	accumulator.finalResponse = response
}

func responseHasResultData(response map[string]any) bool {
	if output, ok := response[jsonOutputKey].([]any); ok && len(output) > 0 {
		return true
	}

	if strings.TrimSpace(stringValue(response["output_text"])) != "" {
		return true
	}

	return false
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
	for _, key := range []string{sseItemIDKey, sseOutputItemIDKey, "id"} {
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

func parseSSEResult(reader io.Reader, onEvent func(*llm.StreamChunk)) (*providerResult, error) {
	accumulator := newSSEAccumulator()
	if err := scanResponsesSSE(reader, accumulator, onEvent); err != nil {
		return nil, err
	}

	if accumulator.terminalErr != nil {
		return nil, accumulator.terminalErr
	}

	if accumulator.sawTypedResponseEvent && !accumulator.terminal {
		return nil, oops.In("provider").
			Code("responses_stream_incomplete").
			Errorf("provider stream closed before completion")
	}

	fallbackText := strings.TrimSpace(strings.Join(accumulator.parts, ""))

	result := providerResultFromSSEAccumulator(accumulator, fallbackText)
	if len(result.Thinking) == 0 {
		result.Thinking = joinedThinkingDeltas(accumulator.thinkingParts)
	}

	return result, nil
}

func scanResponsesSSE(
	reader io.Reader,
	accumulator *sseAccumulator,
	onEvent func(*llm.StreamChunk),
) error {
	err := scanSSEDataLines(reader, func(data string) error {
		if data == sseDoneData {
			return errSSEDone
		}

		if strings.TrimSpace(data) == "" {
			return nil
		}

		var decoded map[string]any
		if err := decodeSSEJSON(data, &decoded, "responses_stream_decode"); err != nil {
			return err
		}

		return accumulator.add(decoded, onEvent)
	})
	if errors.Is(err, errSSEDone) {
		return nil
	}

	return err
}

func providerResultFromSSEAccumulator(accumulator *sseAccumulator, fallbackText string) *providerResult {
	if accumulator.finalResponse != nil {
		return providerResultFromSSEFinalResponse(accumulator, fallbackText)
	}

	if len(accumulator.items) > 0 {
		return providerResultFromOutputItems(accumulator.items, fallbackText)
	}

	return &providerResult{
		FinishReason: llm.FinishReasonUnknown,
		Termination: llm.TerminationMetadata{
			ProviderStatus:       "",
			ProviderFinishReason: "",
			IncompleteReason:     "",
		},
		Text:        fallbackText,
		OutputItems: nil,
		Thinking:    nil,
		ToolCalls:   nil,
		Usage:       llm.EmptyUsage(),
	}
}

func providerResultFromSSEFinalResponse(accumulator *sseAccumulator, fallbackText string) *providerResult {
	result := providerResultFromResponse(accumulator.finalResponse)
	if len(result.OutputItems) == 0 && len(accumulator.items) > 0 {
		usage := result.Usage
		finishReason := result.FinishReason
		result = providerResultFromOutputItems(accumulator.items, fallbackText)

		result.Usage = usage
		if result.FinishReason == llm.FinishReasonUnknown {
			result.FinishReason = finishReason
		}
	}

	if strings.TrimSpace(result.Text) == "" {
		result.Text = fallbackText
	}

	return result
}

func sseProviderError(code string, event map[string]any, fallback string) error {
	message := fallback

	response := map[string]any(nil)
	if value, ok := event["response"].(map[string]any); ok {
		response = value
	}

	if responseMessage := sseErrorMessage(response); responseMessage != "" {
		message = responseMessage
	}

	if eventMessage := sseErrorMessage(event); eventMessage != "" {
		message = eventMessage
	}

	builder := oops.In("provider").Code(code)

	errorDetails := sseErrorDetails(response, event)
	if errorDetails.Type != "" {
		builder = builder.With(ProviderTypeContextKey, errorDetails.Type)
	}

	if errorDetails.Code != "" {
		builder = builder.With(ProviderCodeContextKey, errorDetails.Code)
	}

	responseID := firstNonEmptyString(stringValue(response["id"]), stringValue(event["response_id"]))
	if responseID != "" {
		builder = builder.With(ProviderResponseIDContextKey, responseID)
	}

	requestID := firstNonEmptyString(stringValue(event["request_id"]), stringValue(response["request_id"]))
	if requestID != "" {
		builder = builder.With(ProviderRequestIDContextKey, requestID)
	}

	return builder.Errorf("%s", message)
}

func sseErrorDetails(objects ...map[string]any) ErrorDetails {
	merged := emptyProviderErrorDetails()

	for _, object := range objects {
		details := sseObjectErrorDetails(object)
		merged.Message = firstNonEmptyString(merged.Message, details.Message)
		merged.Type = firstNonEmptyString(merged.Type, details.Type)
		merged.Code = firstNonEmptyString(merged.Code, details.Code)
		merged.Param = firstNonEmptyString(merged.Param, details.Param)
	}

	return merged
}

func sseObjectErrorDetails(object map[string]any) ErrorDetails {
	if object == nil {
		return emptyProviderErrorDetails()
	}

	if nested, ok := object["error"].(map[string]any); ok {
		if details := providerErrorDetailsFromMap(nested); providerErrorDetailsPresent(&details) {
			return details
		}
	}

	return providerErrorDetailsFromMap(object)
}

func providerErrorDetailsPresent(details *ErrorDetails) bool {
	return details != nil && (details.Message != "" || details.Type != "" || details.Code != "")
}

func providerErrorDetailsFromMap(object map[string]any) ErrorDetails {
	content, err := json.Marshal(object)
	if err != nil {
		return emptyProviderErrorDetails()
	}

	details, _ := providerErrorDetailsFromJSON(content)

	return details
}

func sseErrorMessage(object map[string]any) string {
	message := errorMessageFromMap(object)
	if message != "" {
		return message
	}

	if details, ok := object["incomplete_details"].(map[string]any); ok {
		if reason := stringValue(details["reason"]); reason != "" {
			return "provider response incomplete: " + reason
		}
	}

	return ""
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
	if deltaText, ok := event[anthropicDeltaKey].(string); ok {
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
