package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	projectionEntryID = "entry-id"
	projectionNotice  = "notice"
)

type projectionTestCase struct {
	timestamp   time.Time
	wantCreated time.Time
	name        string
	role        Role
	customType  string
	wantSender  string
	wantContent string
	parts       []MessagePartEntity
}

func TestProjectSessionMessage(t *testing.T) {
	t.Parallel()

	entryCreatedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	messageCreatedAt := entryCreatedAt.Add(time.Second)
	imageData := []byte{1, 2, 3}

	tests := []projectionTestCase{
		{
			timestamp: messageCreatedAt, wantCreated: messageCreatedAt,
			name: "ordered text projection around image", role: RoleUser, customType: "",
			wantSender: string(RoleUser), wantContent: "first second",
			parts: []MessagePartEntity{
				projectionTextPart("first "),
				projectionImagePart(imageData, "screen.png"),
				projectionTextPart("second"),
			},
		},
		{
			timestamp: time.Time{}, wantCreated: entryCreatedAt,
			name: "custom sender", role: RoleCustom, customType: "extension.notice",
			wantSender: "extension.notice", wantContent: projectionNotice,
			parts: []MessagePartEntity{projectionTextPart(projectionNotice)},
		},
		{
			timestamp: time.Time{}, wantCreated: entryCreatedAt,
			name: "blank custom type preserves role sender", role: RoleCustom, customType: "",
			wantSender: string(RoleCustom), wantContent: projectionNotice,
			parts: []MessagePartEntity{projectionTextPart(projectionNotice)},
		},
		{
			timestamp: time.Time{}, wantCreated: entryCreatedAt,
			name: "image only", role: RoleUser, customType: "",
			wantSender: string(RoleUser), wantContent: "",
			parts: []MessagePartEntity{projectionImagePart(imageData, "")},
		},
		{
			timestamp: time.Time{}, wantCreated: entryCreatedAt,
			name: "nil parts", role: RoleAssistant, customType: "",
			wantSender: string(RoleAssistant), wantContent: "", parts: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertProjectedSessionMessage(t, entryCreatedAt, &test)
		})
	}
}

func projectionTextPart(text string) MessagePartEntity {
	return MessagePartEntity{
		Text: text, MIMEType: "", Name: "", Type: MessagePartText, Data: nil, Width: 0, Height: 0,
	}
}

func projectionImagePart(data []byte, name string) MessagePartEntity {
	return MessagePartEntity{
		Text: "", MIMEType: "image/png", Name: name, Type: MessagePartImage,
		Data: data, Width: 2, Height: 3,
	}
}

func assertProjectedSessionMessage(t *testing.T, entryCreatedAt time.Time, test *projectionTestCase) {
	t.Helper()

	entry := EntryEntity{
		CreatedAt: entryCreatedAt, ParentID: nil, ToolStatus: "", SessionID: "session-id",
		ToolArgsJSON: "", CustomType: test.customType, DataJSON: "", ID: projectionEntryID,
		Summary: "", ToolName: "", Type: EntryTypeMessage, BranchFromEntryID: "",
		CompactionFirstKeptEntryID: "",
		Message: MessageEntity{
			Timestamp: test.timestamp, Role: test.role, Content: "stale scalar content",
			Provider: "provider", Model: "model", Parts: nil,
		},
		CompactionTokensBefore: 0, TokenEstimate: 0, Display: false, ModelFacing: false,
	}

	got := projectSessionMessage(&entry, test.parts)

	assert.Equal(t, test.wantCreated, got.CreatedAt)
	assert.Equal(t, "session-id", got.SessionID)
	assert.Equal(t, projectionEntryID, got.EntryID)
	assert.Equal(t, test.wantSender, got.Sender)
	assert.Equal(t, test.role, got.Role)
	assert.Equal(t, test.wantContent, got.Content)
	assert.Equal(t, "provider", got.Provider)
	assert.Equal(t, "model", got.Model)
	assert.Equal(t, test.parts, got.Parts)

	if len(got.Parts) > 0 {
		got.Parts[0].Text = "mutated"
		assert.NotEqual(t, "mutated", test.parts[0].Text)
	}

	if len(got.Parts) > 0 && len(got.Parts[0].Data) > 0 {
		got.Parts[0].Data[0] = 9
		assert.Equal(t, byte(1), test.parts[0].Data[0])
	}
}
