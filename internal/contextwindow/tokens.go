package contextwindow

import (
	"strings"
	"unicode/utf8"

	"github.com/omarluq/librecode/internal/database"
)

const (
	charsPerEstimatedToken = 4
	imageTilePixels        = 512
	imageTileTokens        = 200
	minimumImageTokens     = 256
	maximumImageTokens     = 16_000
)

// EstimateTokens returns a rough cross-provider estimate used until provider usage arrives.
func EstimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}

	runes := utf8.RuneCountInString(trimmed)
	if runes == 0 {
		return 0
	}

	return max(1, (runes+charsPerEstimatedToken-1)/charsPerEstimatedToken)
}

// EstimateInputTokens estimates the model-facing input tokens for system prompt plus messages.
func EstimateInputTokens(systemPrompt string, messages []database.MessageEntity) int {
	count := EstimateTokens(systemPrompt)
	for index := range messages {
		count += estimateMessageTokens(&messages[index])
	}

	return count
}

// EstimateMessageTokens estimates the model-facing token count for messages.
func EstimateMessageTokens(messages []database.MessageEntity) int {
	tokens := 0
	for index := range messages {
		tokens += estimateMessageTokens(&messages[index])
	}

	return tokens
}

func estimateMessageTokens(message *database.MessageEntity) int {
	tokens := EstimateTokens(message.Content)
	for index := range message.Parts {
		part := &message.Parts[index]
		if part.Type == database.MessagePartText {
			// Content is the text projection of multipart messages.
			continue
		}

		if part.Type == database.MessagePartImage {
			tokens += estimateImageTokens(part.Width, part.Height)
		}
	}

	return tokens
}

// Images are conservatively estimated in 512px tiles with a fixed floor.
// Malformed dimensions receive the maximum estimate rather than appearing free.
func estimateImageTokens(width, height int) int {
	if width <= 0 || height <= 0 {
		return maximumImageTokens
	}

	tilesWide := width / imageTilePixels
	if width%imageTilePixels != 0 {
		tilesWide++
	}

	tilesHigh := height / imageTilePixels
	if height%imageTilePixels != 0 {
		tilesHigh++
	}

	if tilesWide > maximumImageTokens/imageTileTokens/tilesHigh {
		return maximumImageTokens
	}

	return min(maximumImageTokens, max(minimumImageTokens, tilesWide*tilesHigh*imageTileTokens))
}
