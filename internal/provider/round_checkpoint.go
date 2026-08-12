package provider

import (
	"context"
	"maps"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
)

func runRoundCheckpoint(
	ctx context.Context,
	request *CompletionRequest,
	round *llm.CompletedRound,
) ([]llm.Message, error) {
	if request == nil || request.OnRoundCheckpoint == nil {
		return nil, nil
	}

	checkpointRound := cloneCompletedRound(round)

	messages, err := request.OnRoundCheckpoint(ctx, checkpointRound)
	if err != nil {
		return nil, oops.In("provider").Code("round_checkpoint_failed").
			Wrapf(err, "run provider round checkpoint")
	}

	if err := validateCheckpointMessages(messages); err != nil {
		return nil, err
	}

	return cloneMessages(messages), nil
}

func validateCheckpointMessages(messages []llm.Message) error {
	if err := validateImageMessages(messages); err != nil {
		return err
	}

	for messageIndex := range messages {
		message := &messages[messageIndex]
		if message.Role != llm.RoleUser {
			return oops.In("provider").Code("invalid_round_checkpoint_message").
				With("message_index", messageIndex).
				With("role", message.Role).
				Errorf("round checkpoint returned a non-user message")
		}

		if err := validateCheckpointParts(message, messageIndex); err != nil {
			return err
		}
	}

	return nil
}

func validateCheckpointParts(message *llm.Message, messageIndex int) error {
	content := false

	for partIndex := range message.Content {
		part := &message.Content[partIndex]
		switch part.Type {
		case llm.PartText:
			content = content || strings.TrimSpace(part.Text) != ""
		case llm.PartImage:
			content = true
		case llm.PartReasoning, llm.PartFile, llm.PartSource, llm.PartToolCall, llm.PartToolResult:
			return oops.In("provider").Code("invalid_round_checkpoint_part").
				With("message_index", messageIndex).
				With("part_index", partIndex).
				With("part_type", part.Type).
				Errorf("round checkpoint returned an unsupported user-message part")
		default:
			return oops.In("provider").Code("invalid_round_checkpoint_part").
				With("message_index", messageIndex).
				With("part_index", partIndex).
				With("part_type", part.Type).
				Errorf("round checkpoint returned an unknown user-message part")
		}
	}

	if content {
		return nil
	}

	return oops.In("provider").Code("empty_round_checkpoint_message").
		With("message_index", messageIndex).
		Errorf("round checkpoint returned an empty user message")
}

func completedRound(result *providerResult, toolResults []llm.ToolResult) llm.CompletedRound {
	return llm.CompletedRound{
		Assistant:    assistantMessageFromProviderResult(result),
		ToolResults:  cloneToolResults(toolResults),
		FinishReason: normalizedFinishReason(result),
		Usage:        cloneUsage(providerResultUsage(result)),
	}
}

func assistantMessageFromProviderResult(result *providerResult) llm.Message {
	parts := []llm.Part{}

	if result != nil {
		for _, thinking := range result.Thinking {
			trimmed := strings.TrimSpace(thinking)
			if trimmed == "" {
				continue
			}

			parts = append(parts, llm.Part{
				Metadata: nil, ToolCall: nil, ToolResult: nil,
				Type: llm.PartReasoning, Text: trimmed, Data: "", MIMEType: "",
			})
		}

		if text := strings.TrimSpace(result.Text); text != "" {
			parts = append(parts, llm.TextPart(text))
		}

		toolCalls := toolCallsToLLM(result.ToolCalls)
		for index := range toolCalls {
			parts = append(parts, llm.Part{
				Metadata: nil, ToolCall: &toolCalls[index], ToolResult: nil,
				Type: llm.PartToolCall, Text: "", Data: "", MIMEType: "",
			})
		}
	}

	return llm.Message{Metadata: nil, Role: llm.RoleAssistant, Content: parts}
}

func completedRoundWithToolEvents(result *providerResult, events []ToolEvent) llm.CompletedRound {
	toolResults := make([]llm.ToolResult, 0, len(events))
	for index := range events {
		toolResult := toolResultFromEvent(&events[index])
		if result != nil && index < len(result.ToolCalls) {
			toolResult.ToolCallID = result.ToolCalls[index].ID
		}

		toolResults = append(toolResults, *toolResult)
	}

	return completedRound(result, toolResults)
}

func cloneCompletedRound(round *llm.CompletedRound) *llm.CompletedRound {
	if round == nil {
		return &llm.CompletedRound{
			Assistant: llm.Message{Metadata: nil, Role: "", Content: nil}, ToolResults: nil,
			FinishReason: llm.FinishReasonUnknown, Usage: llm.EmptyUsage(),
		}
	}

	return &llm.CompletedRound{
		Assistant:    cloneMessage(round.Assistant),
		ToolResults:  cloneToolResults(round.ToolResults),
		FinishReason: round.FinishReason,
		Usage:        cloneUsage(round.Usage),
	}
}

func cloneMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}

	cloned := make([]llm.Message, len(messages))
	for index := range messages {
		cloned[index] = cloneMessage(messages[index])
	}

	return cloned
}

func cloneMessage(message llm.Message) llm.Message {
	return llm.Message{
		Metadata: cloneAnyMap(message.Metadata),
		Role:     message.Role,
		Content:  cloneParts(message.Content),
	}
}

func cloneParts(parts []llm.Part) []llm.Part {
	if len(parts) == 0 {
		return nil
	}

	cloned := make([]llm.Part, len(parts))
	for index := range parts {
		cloned[index] = parts[index]
		cloned[index].Metadata = cloneAnyMap(parts[index].Metadata)
		cloned[index].ToolCall = cloneToolCall(parts[index].ToolCall)
		cloned[index].ToolResult = cloneToolResult(parts[index].ToolResult)
	}

	return cloned
}

func cloneToolCall(call *llm.ToolCall) *llm.ToolCall {
	if call == nil {
		return nil
	}

	cloned := *call
	cloned.Metadata = cloneAnyMap(call.Metadata)

	return &cloned
}

func cloneToolResult(result *llm.ToolResult) *llm.ToolResult {
	if result == nil {
		return nil
	}

	cloned := *result
	cloned.Metadata = cloneAnyMap(result.Metadata)
	cloned.Content = cloneParts(result.Content)

	return &cloned
}

func cloneToolResults(results []llm.ToolResult) []llm.ToolResult {
	if len(results) == 0 {
		return nil
	}

	cloned := make([]llm.ToolResult, len(results))
	for index := range results {
		cloned[index] = *cloneToolResult(&results[index])
	}

	return cloned
}

func cloneUsage(usage llm.Usage) llm.Usage {
	cloned := usage
	cloned.Breakdown = cloneStringIntMap(usage.Breakdown)

	cloned.TopContributors = append([]llm.TokenContributor(nil), usage.TopContributors...)
	if usage.Reported() {
		cloned = cloned.WithReported()
	}

	return cloned
}

func cloneStringIntMap(values map[string]int) map[string]int {
	if values == nil {
		return nil
	}

	cloned := make(map[string]int, len(values))
	maps.Copy(cloned, values)

	return cloned
}

func normalizedFinishReason(result *providerResult) llm.FinishReason {
	if result == nil || result.FinishReason == llm.FinishReasonUnknown {
		return llm.FinishReasonStop
	}

	return result.FinishReason
}

func providerResultUsage(result *providerResult) llm.Usage {
	if result == nil {
		return llm.EmptyUsage()
	}

	return result.Usage
}
