package database_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/testutil"
)

const (
	testImageMIME     = "image/png"
	testMessageText   = "text"
	testModel         = "model"
	testMultipartText = "compare these"
	testStoredScalar  = "stored scalar"
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
			Data: nil, Text: "compare ", MIMEType: "", Name: "",
			Type: database.MessagePartText, Width: 0, Height: 0,
		},
		{
			Data: originalData, Text: "", MIMEType: testImageMIME, Name: "first.png",
			Type: database.MessagePartImage, Width: 10, Height: 20,
		},
		{
			Data: nil, Text: "these", MIMEType: "", Name: "",
			Type: database.MessagePartText, Width: 0, Height: 0,
		},
		{
			Data: []byte{4, 5}, Text: "", MIMEType: "image/jpeg", Name: "second.jpg",
			Type: database.MessagePartImage, Width: 30, Height: 40,
		},
	}
	timestamp := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	entry, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: timestamp, Role: database.RoleUser, Content: testMultipartText,
		Provider: "provider", Model: testModel, Parts: parts,
	})
	require.NoError(t, err)

	// The repository owns its bytes after append.
	originalData[0] = 99
	parts[1].Data[1] = 99

	expected := database.MessageEntity{
		Timestamp: timestamp, Role: database.RoleUser, Content: testMultipartText,
		Provider: "provider", Model: testModel, Parts: expectedMultipartParts(),
	}

	messages, err := repository.Messages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assertSessionMessageHydrated(t, &messages[0], session.ID, entry.ID, string(database.RoleUser), &expected)

	transcript, err := repository.TranscriptMessages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, transcript, 1)
	assertSessionMessageHydrated(t, &transcript[0], session.ID, entry.ID, string(database.RoleUser), &expected)

	message, found, err := repository.MessageForEntry(ctx, session.ID, entry.ID)
	require.NoError(t, err)
	require.True(t, found)
	assertSessionMessageHydrated(t, message, session.ID, entry.ID, string(database.RoleUser), &expected)

	assertEntryReadersHydrateMultipart(t, repository, session.ID, entry.ID, &expected)

	contextEntity, err := repository.BuildContext(ctx, session.ID, entry.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 1)
	assert.Equal(t, expected, contextEntity.Messages[0])

	// Mutating one read cannot mutate durable data returned by a later read.
	messages[0].Parts[1].Data[0] = 88
	again, err := repository.Messages(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte{1, 2, 3}, again[0].Parts[1].Data)
}

func TestSessionRepository_CustomSenderAndDisplayFilteringPreserveHydratedMessages(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "custom sender", "")
	require.NoError(t, err)

	hidden, err := repository.AppendCustomMessage(
		ctx, session.ID, nil, "extension-context", "hidden context", false, nil,
	)
	require.NoError(t, err)
	visible, err := repository.AppendCustomMessage(
		ctx, session.ID, &hidden.ID, "extension-context", "visible context", true, nil,
	)
	require.NoError(t, err)

	messages, err := repository.Messages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, []string{"hidden context", "visible context"}, []string{messages[0].Content, messages[1].Content})

	for index := range messages {
		assert.Equal(t, "extension-context", messages[index].Sender)
		assert.Equal(t, database.RoleCustom, messages[index].Role)
		require.Len(t, messages[index].Parts, 1)
		assert.Equal(t, database.MessagePartText, messages[index].Parts[0].Type)
		assert.Equal(t, messages[index].Content, messages[index].Parts[0].Text)
	}

	transcript, err := repository.TranscriptMessages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, transcript, 1)
	assert.Equal(t, visible.ID, transcript[0].EntryID)
	assert.Equal(t, "extension-context", transcript[0].Sender)
	assert.Equal(t, messages[1].Parts, transcript[0].Parts)
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

func TestSessionRepository_CanonicalizesScalarTextAndPersistsImageOnly(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "compatibility", "")
	require.NoError(t, err)

	text, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "canonical text",
		Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)
	imageOnly, err := repository.AppendMessage(ctx, session.ID, &text.ID, &database.MessageEntity{
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

	canonicalParts := []database.MessagePartEntity{{
		Data: nil, Text: "canonical text", MIMEType: "", Name: "",
		Type: database.MessagePartText, Width: 0, Height: 0,
	}}
	assert.Equal(t, canonicalParts, messages[0].Parts)
	assert.Empty(t, messages[1].Content)
	require.Len(t, messages[1].Parts, 1)
	assert.Equal(t, database.MessagePartImage, messages[1].Parts[0].Type)

	contextEntity, err := repository.BuildContext(ctx, session.ID, imageOnly.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 2)
	assert.Empty(t, contextEntity.Messages[1].Content)
	assert.Equal(t, []byte{7}, contextEntity.Messages[1].Parts[0].Data)
}

func TestSessionRepository_SurfacesMalformedAndMissingCanonicalPartsWithoutSynthesis(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		mutate      string
		wantContent string
		wantParts   []database.MessagePartEntity
	}{
		{
			name: "malformed text part",
			mutate: `UPDATE session_message_parts
SET text = '', data = X'01'
WHERE entry_id = ? AND sequence = 0`,
			wantContent: "",
			wantParts: []database.MessagePartEntity{{
				Data: []byte{1}, Text: "", MIMEType: "", Name: "",
				Type: database.MessagePartText, Width: 0, Height: 0,
			}},
		},
		{
			name:        "missing canonical part",
			mutate:      `DELETE FROM session_message_parts WHERE entry_id = ?`,
			wantParts:   nil,
			wantContent: "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			connection, err := sql.Open(sqliteDriver(), ":memory:")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, connection.Close()) })
			connection.SetMaxOpenConns(1)

			ctx := context.Background()
			require.NoError(t, database.Migrate(ctx, connection))
			repository := testutil.SessionRepository(t, connection)
			session, err := repository.CreateSession(ctx, "/work", test.name, "")
			require.NoError(t, err)
			entry, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
				Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: testStoredScalar,
				Provider: "", Model: "", Parts: nil,
			})
			require.NoError(t, err)

			_, err = connection.ExecContext(ctx, test.mutate, entry.ID)
			require.NoError(t, err)

			assertAllMessageReaders(t, repository, session.ID, entry.ID, func(message *database.MessageEntity) {
				assert.Equal(t, test.wantContent, message.Content)
				assert.Equal(t, test.wantParts, message.Parts)
			})
		})
	}
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

func TestSessionRepository_NormalizesScalarContentFromCanonicalParts(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := t.Context()
	session, err := repository.CreateSession(ctx, "/work", "canonical parts", "")
	require.NoError(t, err)
	entry, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "stale scalar",
		Provider: "", Model: "", Parts: []database.MessagePartEntity{testTextPart(testMessageText, nil)},
	})
	require.NoError(t, err)

	assertAllMessageReaders(t, repository, session.ID, entry.ID, func(message *database.MessageEntity) {
		assert.Equal(t, testMessageText, message.Content)
		require.Len(t, message.Parts, 1)
		assert.Equal(t, testMessageText, message.Parts[0].Text)
	})
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

func TestSessionRepository_PartInsertFailureRollsBackEntryEnvelopePartsAndSessionTouch(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)

	ctx := context.Background()
	require.NoError(t, database.Migrate(ctx, connection))
	require.NoError(t, database.ConfigureSQLite(ctx, connection, database.SQLiteOptions{BusyTimeout: 0}))
	_, err = connection.ExecContext(ctx, `CREATE TRIGGER reject_second_message_part
BEFORE INSERT ON session_message_parts
WHEN NEW.sequence = 1
BEGIN
    SELECT RAISE(ABORT, 'reject second message part');
END`)
	require.NoError(t, err)

	repository := testutil.SessionRepository(t, connection)
	session, err := repository.CreateSession(ctx, "/work", "rollback", "")
	require.NoError(t, err)

	_, err = repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: session.UpdatedAt.Add(time.Hour), Role: database.RoleUser, Content: "before failure",
		Provider: "", Model: "", Parts: []database.MessagePartEntity{
			{
				Data: nil, Text: "before failure", MIMEType: "", Name: "",
				Type: database.MessagePartText, Width: 0, Height: 0,
			},
			{
				Data: []byte{1}, Text: "", MIMEType: testImageMIME, Name: "test.png",
				Type: database.MessagePartImage, Width: 1, Height: 1,
			},
		},
	})
	require.ErrorContains(t, err, "reject second message part")

	for _, table := range []string{"session_entries", "session_messages", "session_message_parts"} {
		var count int
		require.NoError(t, connection.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		assert.Zero(t, count, table)
	}

	after, found, err := repository.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, session.UpdatedAt, after.UpdatedAt)
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
	repository := testutil.SessionRepository(t, connection)

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
	expected *database.MessageEntity,
) {
	t.Helper()

	ctx := context.Background()

	branch, err := repository.Branch(ctx, sessionID, entryID)
	require.NoError(t, err)
	require.Len(t, branch, 1)
	assert.Equal(t, *expected, branch[0].Message)

	entries, err := repository.Entries(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, *expected, entries[0].Message)

	loadedEntry, found, err := repository.Entry(ctx, sessionID, entryID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, *expected, loadedEntry.Message)

	leaf, found, err := repository.LeafEntry(ctx, sessionID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, *expected, leaf.Message)

	children, err := repository.Children(ctx, sessionID, nil)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, *expected, children[0].Message)

	tree, err := repository.Tree(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	assert.Equal(t, *expected, tree[0].Entry.Message)
}

func assertAllMessageReaders(
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	entryID string,
	assertMessage func(*database.MessageEntity),
) {
	t.Helper()

	ctx := context.Background()

	branch, err := repository.Branch(ctx, sessionID, entryID)
	require.NoError(t, err)
	require.Len(t, branch, 1)
	assertMessage(&branch[0].Message)

	entries, err := repository.Entries(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assertMessage(&entries[0].Message)

	entry, found, err := repository.Entry(ctx, sessionID, entryID)
	require.NoError(t, err)
	require.True(t, found)
	assertMessage(&entry.Message)

	leaf, found, err := repository.LeafEntry(ctx, sessionID)
	require.NoError(t, err)
	require.True(t, found)
	assertMessage(&leaf.Message)

	children, err := repository.Children(ctx, sessionID, nil)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assertMessage(&children[0].Message)

	tree, err := repository.Tree(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	assertMessage(&tree[0].Entry.Message)

	messages, err := repository.Messages(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assertMessage(messageEntityFromSessionMessage(&messages[0]))

	transcript, err := repository.TranscriptMessages(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, transcript, 1)
	assertMessage(messageEntityFromSessionMessage(&transcript[0]))

	message, found, err := repository.MessageForEntry(ctx, sessionID, entryID)
	require.NoError(t, err)
	require.True(t, found)
	assertMessage(messageEntityFromSessionMessage(message))
}

func messageEntityFromSessionMessage(message *database.SessionMessageEntity) *database.MessageEntity {
	return &database.MessageEntity{
		Timestamp: message.CreatedAt, Role: message.Role, Content: message.Content,
		Provider: message.Provider, Model: message.Model, Parts: message.Parts,
	}
}

func assertSessionMessageHydrated(
	t *testing.T,
	message *database.SessionMessageEntity,
	sessionID string,
	entryID string,
	sender string,
	expected *database.MessageEntity,
) {
	t.Helper()

	assert.Equal(t, sessionID, message.SessionID)
	assert.Equal(t, entryID, message.EntryID)
	assert.Equal(t, sender, message.Sender)
	assert.Equal(t, expected.Timestamp, message.CreatedAt)
	assert.Equal(t, expected.Role, message.Role)
	assert.Equal(t, expected.Content, message.Content)
	assert.Equal(t, expected.Provider, message.Provider)
	assert.Equal(t, expected.Model, message.Model)
	assert.Equal(t, expected.Parts, message.Parts)
}

func expectedMultipartParts() []database.MessagePartEntity {
	return []database.MessagePartEntity{
		{
			Data: nil, Text: "compare ", MIMEType: "", Name: "",
			Type: database.MessagePartText, Width: 0, Height: 0,
		},
		{
			Data: []byte{1, 2, 3}, Text: "", MIMEType: testImageMIME, Name: "first.png",
			Type: database.MessagePartImage, Width: 10, Height: 20,
		},
		{
			Data: nil, Text: "these", MIMEType: "", Name: "",
			Type: database.MessagePartText, Width: 0, Height: 0,
		},
		{
			Data: []byte{4, 5}, Text: "", MIMEType: "image/jpeg", Name: "second.jpg",
			Type: database.MessagePartImage, Width: 30, Height: 40,
		},
	}
}
