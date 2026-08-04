package provider

import (
	"encoding/base64"
	"fmt"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
)

func validateImageMessages(messages []llm.Message) error {
	for messageIndex := range messages {
		message := &messages[messageIndex]
		for partIndex := range message.Content {
			part := &message.Content[partIndex]
			if err := validateImagePart(message.Role, part, messageIndex, partIndex); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateImagePart(role llm.Role, part *llm.Part, messageIndex, partIndex int) error {
	if part.Type != llm.PartImage {
		return nil
	}

	if role != llm.RoleUser {
		return imageConversionError(messageIndex, partIndex, "image content is only supported for user messages")
	}

	if part.Data == "" {
		return imageConversionError(messageIndex, partIndex, "image content requires base64 data")
	}

	if !supportedImageMIMEType(part.MIMEType) {
		return imageConversionError(messageIndex, partIndex, "unsupported image MIME type")
	}

	if _, err := base64.StdEncoding.DecodeString(part.Data); err != nil {
		return imageConversionError(messageIndex, partIndex, "image content contains malformed base64 data")
	}

	return nil
}

func supportedImageMIMEType(mimeType string) bool {
	switch mimeType {
	case "image/gif", "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func imageConversionError(messageIndex, partIndex int, text string) error {
	return oops.In("provider").Code("invalid_image_content").
		With("message_index", messageIndex).With("part_index", partIndex).Errorf("%s", text)
}

const (
	jsonImageURLKey       = "image_url"
	jsonImageURLValueKey  = "url"
	openAIInputImageType  = "input_image"
	anthropicImageType    = "image"
	anthropicBase64Type   = "base64"
	anthropicSourceKey    = "source"
	anthropicMediaTypeKey = "media_type"
	encodedImageDataKey   = "data"
)

func openAIDataURL(part *llm.Part) string {
	return fmt.Sprintf("data:%s;base64,%s", part.MIMEType, part.Data)
}

func openAIResponseUserContent(message llm.Message) []map[string]any {
	blocks := make([]map[string]any, 0, len(message.Content))
	for index := range message.Content {
		part := &message.Content[index]
		switch part.Type {
		case llm.PartText:
			if part.Text != "" {
				blocks = append(blocks, map[string]any{jsonTypeKey: "input_text", jsonTextKey: part.Text})
			}
		case llm.PartImage:
			blocks = append(blocks, map[string]any{
				jsonTypeKey: openAIInputImageType, jsonImageURLKey: openAIDataURL(part),
			})
		case llm.PartReasoning, llm.PartFile, llm.PartSource, llm.PartToolCall, llm.PartToolResult:
			continue
		}
	}

	return blocks
}

func openAIChatUserContent(message llm.Message) any {
	hasImage := false
	for index := range message.Content {
		hasImage = hasImage || message.Content[index].Type == llm.PartImage
	}

	if !hasImage {
		return messageText(message)
	}

	blocks := make([]map[string]any, 0, len(message.Content))
	for index := range message.Content {
		part := &message.Content[index]
		switch part.Type {
		case llm.PartText:
			if part.Text != "" {
				blocks = append(blocks, map[string]any{jsonTypeKey: jsonTextKey, jsonTextKey: part.Text})
			}
		case llm.PartImage:
			blocks = append(blocks, map[string]any{
				jsonTypeKey:     jsonImageURLKey,
				jsonImageURLKey: map[string]any{jsonImageURLValueKey: openAIDataURL(part)},
			})
		case llm.PartReasoning, llm.PartFile, llm.PartSource, llm.PartToolCall, llm.PartToolResult:
			continue
		}
	}

	return blocks
}

func anthropicUserContent(message llm.Message) any {
	hasImage := false
	for index := range message.Content {
		hasImage = hasImage || message.Content[index].Type == llm.PartImage
	}

	if !hasImage {
		return messageText(message)
	}

	blocks := make([]map[string]any, 0, len(message.Content))
	for index := range message.Content {
		part := &message.Content[index]
		switch part.Type {
		case llm.PartText:
			if part.Text != "" {
				blocks = append(blocks, map[string]any{jsonTypeKey: jsonTextKey, jsonTextKey: part.Text})
			}
		case llm.PartImage:
			blocks = append(blocks, map[string]any{jsonTypeKey: anthropicImageType, anthropicSourceKey: map[string]any{
				jsonTypeKey: anthropicBase64Type, anthropicMediaTypeKey: part.MIMEType, encodedImageDataKey: part.Data,
			}})
		case llm.PartReasoning, llm.PartFile, llm.PartSource, llm.PartToolCall, llm.PartToolResult:
			continue
		}
	}

	return blocks
}
