package assistant

import (
	"time"

	"github.com/omarluq/librecode/internal/database"
)

func testMessageEntity(role database.Role, content string) database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Time{},
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	}
}
