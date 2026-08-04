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

const testImageMIME = "image/png"

func TestSessionRepository_RoundTripsOrderedMultipartMessages(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "multipart", "")
	require.NoError(t, err)

	originalData := []byte{1, 2, 3}
	parts := []database.MessagePartEntity{
		{Data: nil, Text: "compare these", MIMEType: "", Name: "", Type: database.MessagePartText, Width: 0, Height: 0},
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

	branch, err := repository.Branch(ctx, session.ID, entry.ID)
	require.NoError(t, err)
	require.Len(t, branch, 1)
	assertMultipartParts(t, branch[0].Message.Parts)

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

func TestSessionRepository_RejectsMultipartResourceLimitBypasses(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "limits", "")
	require.NoError(t, err)

	images := make([]database.MessagePartEntity, 5)
	for index := range images {
		images[index] = database.MessagePartEntity{
			Text: "", MIMEType: testImageMIME, Name: "", Type: database.MessagePartImage,
			Data: []byte{1}, Width: 1, Height: 1,
		}
	}

	_, err = repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "", Provider: "", Model: "", Parts: images,
	})
	require.ErrorContains(t, err, "maximum is 4")

	_, err = repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "", Provider: "", Model: "",
		Parts: []database.MessagePartEntity{{
			Text: "", MIMEType: testImageMIME, Name: "", Type: database.MessagePartImage,
			Data: []byte{1}, Width: 40_000_001, Height: 1,
		}},
	})
	require.ErrorContains(t, err, "40 megapixels")

	entries, listErr := repository.Entries(ctx, session.ID)
	require.NoError(t, listErr)
	assert.Empty(t, entries)
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

	repository := database.NewSessionRepository(connection)
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
	repository := database.NewSessionRepository(connection)

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

func assertMultipartParts(t *testing.T, parts []database.MessagePartEntity) {
	t.Helper()
	require.Len(t, parts, 3)
	assert.Equal(t, database.MessagePartText, parts[0].Type)
	assert.Equal(t, "compare these", parts[0].Text)
	assert.Equal(t, database.MessagePartImage, parts[1].Type)
	assert.Equal(t, []byte{1, 2, 3}, parts[1].Data)
	assert.Equal(t, "first.png", parts[1].Name)
	assert.Equal(t, database.MessagePartImage, parts[2].Type)
	assert.Equal(t, []byte{4, 5}, parts[2].Data)
}
