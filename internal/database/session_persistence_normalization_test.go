package database_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestSessionPersistence_TranscriptPaginationHasNoTieOrBoundaryGaps(t *testing.T) {
	t.Parallel()

	connection, repository := newNormalizationRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "pagination", "")
	require.NoError(t, err)
	other, err := repository.CreateSession(ctx, "/other", "isolation", "")
	require.NoError(t, err)

	stamp := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)

	var parent *string

	for index := range 8 {
		display := index != 2 && index != 5
		parts := []database.MessagePartEntity(nil)

		content := string(rune('a' + index))
		if index == 4 {
			content = ""
			parts = []database.MessagePartEntity{testImagePart([]byte{byte(index)}, "", 1, 1)}
		}

		entry, appendErr := repository.AppendMessageWithDisplay(ctx, session.ID, parent, &database.MessageEntity{
			Timestamp: stamp, Role: database.RoleUser, Content: content, Provider: "", Model: "", Parts: parts,
		}, nil, &display)
		require.NoError(t, appendErr)

		parent = &entry.ID
	}

	_, err = repository.AppendMessage(ctx, other.ID, nil, &database.MessageEntity{
		Timestamp: stamp, Role: database.RoleUser, Content: "foreign", Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)

	full, err := repository.TranscriptMessages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, full, 6)
	assert.Empty(t, full[3].Content)
	require.Len(t, full[3].Parts, 1)

	for _, limit := range []int{1, 2, 99} {
		tail, tailErr := repository.TranscriptMessageTail(ctx, session.ID, limit)
		require.NoError(t, tailErr)

		collected := slices.Clone(tail)
		for len(collected) < len(full) {
			cursor := collected[0]
			page, pageErr := repository.TranscriptMessagesBefore(
				ctx, session.ID, cursor.CreatedAt, cursor.EntryID, limit,
			)
			require.NoError(t, pageErr)
			require.NotEmpty(t, page)
			collected = append(page, collected...)
		}

		assert.Equal(t, full, collected, "limit %d", limit)
	}

	zeroTail, err := repository.TranscriptMessageTail(ctx, session.ID, 0)
	require.NoError(t, err)
	assert.Empty(t, zeroTail)

	zeroBefore, err := repository.TranscriptMessagesBefore(ctx, session.ID, stamp, full[0].EntryID, 0)
	require.NoError(t, err)
	assert.Empty(t, zeroBefore)

	assertTranscriptQueryPlans(t, connection)
}

func TestSessionPersistence_RejectsInvalidParentsAtRepositoryAndDatabaseBoundaries(t *testing.T) {
	t.Parallel()

	connection, repository := newNormalizationRepository(t)
	ctx := context.Background()
	first, err := repository.CreateSession(ctx, "/work", "first", "")
	require.NoError(t, err)
	second, err := repository.CreateSession(ctx, "/work", "second", "")
	require.NoError(t, err)
	parent, err := repository.AppendMessage(ctx, first.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: "parent", Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)

	_, err = repository.AppendMessage(ctx, second.ID, &parent.ID, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   "cross-session",
		Provider:  "",
		Model:     "",
		Parts:     nil,
	})
	require.Error(t, err)
	_, err = repository.CreateSession(ctx, "/work", "orphan", "019755d8-8090-7000-8000-000000000001")
	require.Error(t, err)

	secondRoot, err := repository.AppendMessage(ctx, second.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   "second root",
		Provider:  "",
		Model:     "",
		Parts:     nil,
	})
	require.NoError(t, err)
	_, err = connection.ExecContext(
		ctx, `UPDATE session_entries SET parent_id = ? WHERE id = ?`, parent.ID, secondRoot.ID,
	)
	require.Error(t, err)
}

func TestSessionPersistence_NormalizesMetadataAndPreservesContextState(t *testing.T) {
	t.Parallel()

	_, repository := newNormalizationRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "metadata", "")
	require.NoError(t, err)
	root, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   "root",
		Provider:  "openai",
		Model:     "model",
		Parts:     nil,
	})
	require.NoError(t, err)
	compaction, err := repository.AppendCompaction(ctx, &database.AppendCompactionInput{
		ParentID: &root.ID, Details: map[string]any{"retained": map[string]any{"value": 7}},
		SessionID: session.ID, Summary: "summary", FirstKeptEntryID: root.ID,
		OperationID: "019755d8-8090-7000-8000-000000000002", TokensBefore: 42, FromHook: false,
	})
	require.NoError(t, err)

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(compaction.DataJSON), &data))

	for _, key := range []string{
		"tool_name", "tool_status", "tool_args_json", "token_estimate", "model_facing", "display",
		"compaction_first_kept_entry_id", "first_kept_entry_id", "firstKeptEntryId",
		"compaction_tokens_before", "tokens_before", "tokensBefore",
		"branch_from_entry_id", "from_id", "fromId",
	} {
		assert.NotContains(t, data, key)
	}

	details, ok := data["details"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"value": float64(7)}, details["retained"])
	assert.Equal(t, root.ID, compaction.CompactionFirstKeptEntryID)
	assert.Equal(t, 42, compaction.CompactionTokensBefore)

	contextEntity, err := repository.BuildContext(ctx, session.ID, compaction.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 2)
	assert.Equal(t, database.RoleCompactionSummary, contextEntity.Messages[0].Role)
	assert.Equal(t, "summary", contextEntity.Messages[0].Content)
	assert.Equal(t, database.RoleUser, contextEntity.Messages[1].Role)
	assert.Equal(t, "root", contextEntity.Messages[1].Content)
	assert.Nil(t, contextEntity.UsageAnchor)
}

func TestSessionPersistence_DirectDeletionCascadesSubtreeAndPromotesChildSession(t *testing.T) {
	t.Parallel()

	connection, repository := newNormalizationRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "parent", "")
	require.NoError(t, err)
	childSession, err := repository.CreateSession(ctx, "/work", "child", session.ID)
	require.NoError(t, err)
	sibling, err := repository.AppendMessage(ctx, session.ID, nil, normalizationMessage("sibling"))
	require.NoError(t, err)
	root, err := repository.AppendMessage(ctx, session.ID, nil, normalizationMessage("root"))
	require.NoError(t, err)
	child, err := repository.AppendMessage(ctx, session.ID, &root.ID, normalizationMessage("child"))
	require.NoError(t, err)

	result, err := connection.ExecContext(
		ctx, `DELETE FROM session_entries WHERE id = ? AND session_id = ?`, root.ID, session.ID,
	)
	require.NoError(t, err)
	deleted, err := result.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	for _, id := range []string{root.ID, child.ID} {
		_, found, findErr := repository.Entry(ctx, session.ID, id)
		require.NoError(t, findErr)
		assert.False(t, found)
		_, found, findErr = repository.MessageForEntry(ctx, session.ID, id)
		require.NoError(t, findErr)
		assert.False(t, found)
	}

	_, found, err := repository.Entry(ctx, session.ID, sibling.ID)
	require.NoError(t, err)
	assert.True(t, found)

	require.NoError(t, repository.DeleteSession(ctx, session.ID))
	promoted, found, err := repository.GetSession(ctx, childSession.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Empty(t, promoted.ParentSession)
}

func newNormalizationRepository(t *testing.T) (*sql.DB, *database.SessionRepository) {
	t.Helper()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(t.Context(), connection))

	return connection, testutil.SessionRepository(t, connection)
}

func normalizationMessage(content string) *database.MessageEntity {
	return &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: content, Provider: "", Model: "", Parts: nil,
	}
}

func assertTranscriptQueryPlans(t *testing.T, connection *sql.DB) {
	t.Helper()

	for name, query := range map[string]string{
		"tail": `EXPLAIN QUERY PLAN SELECT entry_id, role FROM (
SELECT e.id AS entry_id, m.role FROM session_entries AS e INDEXED BY idx_session_entries_transcript_cursor
JOIN session_messages AS m ON m.entry_id = e.id
WHERE e.session_id = ? AND e.display = 1 ORDER BY e.created_at DESC, e.id DESC LIMIT ?)
ORDER BY entry_id`,
		"before": `EXPLAIN QUERY PLAN SELECT entry_id, role FROM (
SELECT e.id AS entry_id, m.role FROM session_entries AS e INDEXED BY idx_session_entries_transcript_cursor
JOIN session_messages AS m ON m.entry_id = e.id
WHERE e.session_id = ? AND e.display = 1 AND (e.created_at < ? OR (e.created_at = ? AND e.id < ?))
ORDER BY e.created_at DESC, e.id DESC LIMIT ?)
ORDER BY entry_id`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			args := []any{"session", 2}
			if name == "before" {
				args = []any{"session", "time", "time", "entry", 2}
			}

			rows, err := connection.QueryContext(t.Context(), query, args...)

			require.NoError(t, err)
			defer func() { require.NoError(t, rows.Close()) }()

			usedCursor := false

			for rows.Next() {
				var (
					id, parent, unused int
					detail             string
				)
				require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
				usedCursor = usedCursor || strings.Contains(detail, "idx_session_entries_transcript_cursor")
			}

			require.NoError(t, rows.Err())
			assert.True(t, usedCursor, "query plan should use transcript cursor index")
		})
	}
}
