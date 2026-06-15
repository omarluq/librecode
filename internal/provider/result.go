package provider

import (
	"strings"

	"github.com/omarluq/librecode/internal/llm"
)

func newResponse() *llm.Response {
	return &llm.Response{
		FinishReason: llm.FinishReasonUnknown,
		Content:      nil,
		ToolCalls:    nil,
		Usage:        llm.EmptyUsage(),
	}
}

func appendThinking(response *llm.Response, thinking []string) {
	for _, text := range thinking {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}

		response.Content = append(response.Content, llm.Part{
			Metadata:   nil,
			ToolCall:   nil,
			ToolResult: nil,
			Type:       llm.PartReasoning,
			Text:       trimmed,
			Data:       "",
			MIMEType:   "",
		})
	}
}

func appendToolResults(response *llm.Response, events []ToolEvent) {
	for index := range events {
		response.Content = append(response.Content, toolResultPartFromEvent(&events[index]))
	}
}

func setResponseText(response *llm.Response, text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}

	response.Content = append(response.Content, llm.TextPart(trimmed))
}

func partsText(parts []llm.Part) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part.Text) != "" {
			texts = append(texts, strings.TrimSpace(part.Text))
		}
	}

	return strings.Join(texts, "\n")
}
