package database_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const (
	testImageMIME     = "image/png"
	testMessageText   = "text"
	testMultipartText = "compare these"
)

func TestSessionRepository_RoundTripsOrderedMultipartMessages(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "multipart", "")
	require.NoError(t, err)

	originalData := []byte{1, 2, 3}
	parts := []database.MessagePartEntity{
		{
			Data: nil, Text: testMultipartText, MIMEType: "", Name: "",
			Type: database.MessagePartText, Width: 0, Height: 0,
		},
		{
			Data: originalData, Text: "", MIMEType: testImageMIME, Name: "first.png",
			Type: database.MessagePartImage, Width: 10, Height: 20,
		},
		{
			Data: []byte{4, 5}, Text: "", MIMEType: "image/jpeg", Name: "second.jpg",
			Type: database.MessagePartImage, Width: 30, Height: 40,
		},
	}
	entry, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "compare these",
		Provider: "", Model: "", Parts: parts,
	})
	require.NoError(t, err)

	// The repository owns its bytes after append.
	originalData[0] = 99
	parts[1].Data[1] = 99

	messages, err := repository.Messages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assertMultipartParts(t, messages[0].Parts)

	transcript, err := repository.TranscriptMessages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, transcript, 1)
	assertMultipartParts(t, transcript[0].Parts)

	message, found, err := repository.MessageForEntry(ctx, session.ID, entry.ID)
	require.NoError(t, err)
	require.True(t, found)
	assertMultipartParts(t, message.Parts)

	assertEntryReadersHydrateMultipart(t, repository, session.ID, entry.ID)

	contextEntity, err := repository.BuildContext(ctx, session.ID, entry.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 1)
	assertMultipartParts(t, contextEntity.Messages[0].Parts)

	// Mutating one read cannot mutate durable data returned by a later read.
	messages[0].Parts[1].Data[0] = 88
	again, err := repository.Messages(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte{1, 2, 3}, again[0].Parts[1].Data)
}

func TestSessionRepository_ContextHasImagePartsFollowsActiveBranch(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "image context", "")
	require.NoError(t, err)

	root, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "root", Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)
	image, err := repository.AppendMessage(ctx, session.ID, &root.ID, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "", Provider: "", Model: "",
		Parts: []database.MessagePartEntity{{
			Data: []byte{1}, Text: "", MIMEType: testImageMIME, Name: "",
			Type: database.MessagePartImage, Width: 1, Height: 1,
		}},
	})
	require.NoError(t, err)
	leaf, err := repository.AppendMessage(ctx, session.ID, &image.ID, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleAssistant, Content: "answer",
		Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)

	hasImages, err := repository.ContextHasImageParts(ctx, session.ID, leaf.ID)
	require.NoError(t, err)
	assert.True(t, hasImages)

	hasImages, err = repository.ContextHasImageParts(ctx, session.ID, root.ID)
	require.NoError(t, err)
	assert.False(t, hasImages)

	sibling, err := repository.AppendMessage(ctx, session.ID, &root.ID, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleAssistant, Content: "sibling",
		Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)
	hasImages, err = repository.ContextHasImageParts(ctx, session.ID, sibling.ID)
	require.NoError(t, err)
	assert.False(t, hasImages)

	hasImages, err = repository.ContextHasImageParts(ctx, session.ID, "missing")
	require.NoError(t, err)
	assert.False(t, hasImages)
}

func TestSessionRepository_PersistsImageOnlyAndSupportsLegacyText(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "compatibility", "")
	require.NoError(t, err)

	legacy, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "legacy text",
		Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)
	imageOnly, err := repository.AppendMessage(ctx, session.ID, &legacy.ID, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "", Provider: "", Model: "",
		Parts: []database.MessagePartEntity{{
			Data: []byte{7}, Text: "", MIMEType: testImageMIME, Name: "",
			Type: database.MessagePartImage, Width: 1, Height: 1,
		}},
	})
	require.NoError(t, err)

	messages, err := repository.Messages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	legacyParts := []database.MessagePartEntity{{
		Data: nil, Text: "legacy text", MIMEType: "", Name: "",
		Type: database.MessagePartText, Width: 0, Height: 0,
	}}
	assert.Equal(t, legacyParts, messages[0].Parts)
	assert.Empty(t, messages[1].Content)
	require.Len(t, messages[1].Parts, 1)
	assert.Equal(t, database.MessagePartImage, messages[1].Parts[0].Type)

	contextEntity, err := repository.BuildContext(ctx, session.ID, imageOnly.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 2)
	assert.Empty(t, contextEntity.Messages[1].Content)
	assert.Equal(t, []byte{7}, contextEntity.Messages[1].Parts[0].Data)
}

func TestSessionRepository_RejectsInvalidMultipartMessages(t *testing.T) {
	t.Parallel()

	for _, test := range invalidMultipartMessageCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repository := newTestSessionRepository(t)
			session, err := repository.CreateSession(t.Context(), "/work", test.name, "")
			require.NoError(t, err)
			_, err = repository.AppendMessage(t.Context(), session.ID, nil, &database.MessageEntity{
				Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: test.content, Provider: "", Model: "",
				Parts: test.parts,
			})
			require.ErrorContains(t, err, test.wantErr)

			entries, err := repository.Entries(t.Context(), session.ID)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

type invalidMultipartMessageCase struct {
	name    string
	content string
	wantErr string
	parts   []database.MessagePartEntity
}

func invalidMultipartMessageCases() []invalidMultipartMessageCase {
	images := make([]database.MessagePartEntity, 5)
	for index := range images {
		images[index] = testImagePart([]byte{1}, "", 1, 1)
	}

	return []invalidMultipartMessageCase{
		{name: "image count", content: "", wantErr: "maximum is 4", parts: images},
		{
			name: "text projection mismatch", content: "different",
			wantErr: "content must match the text projection",
			parts:   []database.MessagePartEntity{testTextPart(testMessageText, nil)},
		},
		{
			name: "unsupported type", content: "", wantErr: "unsupported message part type",
			parts: []database.MessagePartEntity{{
				Data: nil, Text: "", MIMEType: "", Name: "",
				Type: database.MessagePartType("audio"), Width: 0, Height: 0,
			}},
		},
		{
			name: "blank text", content: "", wantErr: "text part must have text",
			parts: []database.MessagePartEntity{testTextPart(" \t", nil)},
		},
		{
			name: "text with binary data", content: testMessageText,
			wantErr: "text part must not have binary data",
			parts:   []database.MessagePartEntity{testTextPart(testMessageText, []byte{1})},
		},
		{
			name: "image without binary data", content: "", wantErr: "image part must have binary data",
			parts: []database.MessagePartEntity{testImagePart(nil, "", 1, 1)},
		},
		{
			name: "image with text", content: "", wantErr: "image part must not have text",
			parts: []database.MessagePartEntity{testImagePart([]byte{1}, testMessageText, 1, 1)},
		},
		{
			name: "MIME type", content: "", wantErr: "normalized image MIME type",
			parts: []database.MessagePartEntity{{
				Data: []byte{1}, Text: "", MIMEType: "image/*", Name: "",
				Type: database.MessagePartImage, Width: 1, Height: 1,
			}},
		},
		{
			name: "image byte size", content: "", wantErr: "5 MiB limit",
			parts: []database.MessagePartEntity{testImagePart(make([]byte, 5*1024*1024+1), "", 1, 1)},
		},
		{
			name: "zero width", content: "", wantErr: "dimensions must be positive",
			parts: []database.MessagePartEntity{testImagePart([]byte{1}, "", 0, 1)},
		},
		{
			name: "zero height", content: "", wantErr: "dimensions must be positive",
			parts: []database.MessagePartEntity{testImagePart([]byte{1}, "", 1, 0)},
		},
		{
			name: "pixel count", content: "", wantErr: "40 megapixels",
			parts: []database.MessagePartEntity{testImagePart([]byte{1}, "", 40_000_001, 1)},
		},
	}
}

func testTextPart(text string, data []byte) database.MessagePartEntity {
	return database.MessagePartEntity{
		Data: data, Text: text, MIMEType: "", Name: "",
		Type: database.MessagePartText, Width: 0, Height: 0,
	}
}

func testImagePart(data []byte, text string, width, height int) database.MessagePartEntity {
	return database.MessagePartEntity{
		Data: data, Text: text, MIMEType: testImageMIME, Name: "",
		Type: database.MessagePartImage, Width: width, Height: height,
	}
}

func TestSessionRepository_PartInsertFailureRollsBackEntryAndMessage(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)

	ctx := context.Background()
	require.NoError(t, database.Migrate(ctx, connection))
	require.NoError(t, database.ConfigureSQLite(ctx, connection, database.SQLiteOptions{BusyTimeout: 0}))
	_, err = connection.ExecContext(ctx, `CREATE TRIGGER reject_message_part
BEFORE INSERT ON session_message_parts
BEGIN
    SELECT RAISE(ABORT, 'reject message part');
END`)
	require.NoError(t, err)

	repository := mustSessionRepository(t, connection)
	session, err := repository.CreateSession(ctx, "/work", "rollback", "")
	require.NoError(t, err)

	_, err = repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "", Provider: "", Model: "",
		Parts: []database.MessagePartEntity{{
			Data: []byte{1}, Text: "", MIMEType: testImageMIME, Name: "test.png",
			Type: database.MessagePartImage, Width: 1, Height: 1,
		}},
	})
	require.ErrorContains(t, err, "reject message part")

	entries, err := repository.Entries(ctx, session.ID)
	require.NoError(t, err)
	assert.Empty(t, entries)

	messages, err := repository.Messages(ctx, session.ID)
	require.NoError(t, err)
	assert.Empty(t, messages)
}

func TestSessionRepository_MessagePartsCascadeWithEntryAndSession(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)

	ctx := context.Background()
	require.NoError(t, database.Migrate(ctx, connection))
	require.NoError(t, database.ConfigureSQLite(ctx, connection, database.SQLiteOptions{BusyTimeout: 0}))
	repository := mustSessionRepository(t, connection)

	appendImage := func(name string) (*database.SessionEntity, *database.EntryEntity) {
		session, createErr := repository.CreateSession(ctx, "/work", name, "")
		require.NoError(t, createErr)

		entry, appendErr := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
			Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "", Provider: "", Model: "",
			Parts: []database.MessagePartEntity{{
				Data: []byte{1}, Text: "", MIMEType: testImageMIME, Name: "",
				Type: database.MessagePartImage, Width: 1, Height: 1,
			}},
		})
		require.NoError(t, appendErr)

		return session, entry
	}
	countParts := func() int {
		var count int
		require.NoError(t, connection.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_message_parts`).Scan(&count))

		return count
	}

	firstSession, firstEntry := appendImage("entry cascade")
	secondSession, _ := appendImage("session cascade")

	require.Equal(t, 2, countParts())
	require.NoError(t, repository.DeleteEntryBranch(ctx, firstSession.ID, firstEntry.ID))
	assert.Equal(t, 1, countParts())
	require.NoError(t, repository.DeleteSession(ctx, secondSession.ID))
	assert.Zero(t, countParts())
}

func assertEntryReadersHydrateMultipart(
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	entryID string,
) {
	t.Helper()

	ctx := context.Background()

	branch, err := repository.Branch(ctx, sessionID, entryID)
	require.NoError(t, err)
	require.Len(t, branch, 1)
	assertMultipartParts(t, branch[0].Message.Parts)

	entries, err := repository.Entries(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assertMultipartParts(t, entries[0].Message.Parts)

	loadedEntry, found, err := repository.Entry(ctx, sessionID, entryID)
	require.NoError(t, err)
	require.True(t, found)
	assertMultipartParts(t, loadedEntry.Message.Parts)

	leaf, found, err := repository.LeafEntry(ctx, sessionID)
	require.NoError(t, err)
	require.True(t, found)
	assertMultipartParts(t, leaf.Message.Parts)

	children, err := repository.Children(ctx, sessionID, nil)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assertMultipartParts(t, children[0].Message.Parts)

	tree, err := repository.Tree(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	assertMultipartParts(t, tree[0].Entry.Message.Parts)
}

func assertMultipartParts(t *testing.T, parts []database.MessagePartEntity) {
	t.Helper()
	require.Len(t, parts, 3)
	assert.Equal(t, database.MessagePartEntity{
		Data: nil, Text: testMultipartText, MIMEType: "", Name: "",
		Type: database.MessagePartText, Width: 0, Height: 0,
	}, parts[0])
	assert.Equal(t, database.MessagePartEntity{
		Data: []byte{1, 2, 3}, Text: "", MIMEType: testImageMIME, Name: "first.png",
		Type: database.MessagePartImage, Width: 10, Height: 20,
	}, parts[1])
	assert.Equal(t, database.MessagePartEntity{
		Data: []byte{4, 5}, Text: "", MIMEType: "image/jpeg", Name: "second.jpg",
		Type: database.MessagePartImage, Width: 30, Height: 40,
	}, parts[2])
}
