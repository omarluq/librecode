//nolint:testpackage // These tests validate unexported panel helpers directly.
package terminal

import (
	"time"

	"github.com/omarluq/librecode/internal/database"
)

const (
	testProviderOpenAI = "openai"
	testThemeSetting   = "theme"
)

func testSessionEntity(sessionID, name string) database.SessionEntity {
	return database.SessionEntity{
		CreatedAt:     time.Time{},
		UpdatedAt:     time.Time{},
		CWD:           "",
		ParentSession: "",
		ID:            sessionID,
		Name:          name,
	}
}

func testEntryEntity() database.EntryEntity {
	return database.EntryEntity{
		CreatedAt:                  time.Time{},
		ParentID:                   nil,
		Summary:                    "",
		ToolStatus:                 "",
		Type:                       "",
		CustomType:                 "",
		DataJSON:                   "",
		ID:                         "",
		ToolName:                   "",
		SessionID:                  "",
		ToolArgsJSON:               "",
		BranchFromEntryID:          "",
		CompactionFirstKeptEntryID: "",
		CompactionTokensBefore:     0,
		TokenEstimate:              0,
		Display:                    false,
		ModelFacing:                false,
		Message: database.MessageEntity{
			Timestamp: time.Time{},
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
		},
	}
}
