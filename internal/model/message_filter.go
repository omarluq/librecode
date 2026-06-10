package model

import (
	"time"

	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/database"
)

// FacingMessages filters and converts persisted messages for model replay.
func FacingMessages(messages []database.MessageEntity) []database.MessageEntity {
	return lo.FilterMap(messages, func(message database.MessageEntity, _ int) (database.MessageEntity, bool) {
		if !IsFacingMessage(&message) {
			return emptyMessage(), false
		}

		return FacingMessage(&message), true
	})
}

func emptyMessage() database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Time{},
		Role:      "",
		Content:   "",
		Provider:  "",
		Model:     "",
	}
}
