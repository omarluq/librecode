package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

// appendTestBranchSummary creates a branch summary entry with sensible
// defaults so call sites don't repeat the 7-argument AppendBranchSummary
// signature.
func appendTestBranchSummary(
	ctx context.Context,
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	parentID *string,
	sourceEntryID string,
	summary string,
) *database.EntryEntity {
	t.Helper()

	entry, err := repository.AppendBranchSummary(
		ctx,
		sessionID,
		parentID,
		sourceEntryID,
		summary,
		nil,
		false,
	)
	require.NoError(t, err)

	return entry
}

// appendTestCompaction creates a compaction entry from a fully-specified
// input so call sites don't repeat the 7-field struct literal.
func appendTestCompaction(
	ctx context.Context,
	t *testing.T,
	repository *database.SessionRepository,
	input *database.AppendCompactionInput,
) *database.EntryEntity {
	t.Helper()

	entry, err := repository.AppendCompaction(ctx, input)
	require.NoError(t, err)

	return entry
}

// appendTestCompactionSimple is a convenience wrapper for the most common
// test compaction shape: a parent, a session, a summary, and a first-kept
// entry, with no details or hook.
func appendTestCompactionSimple(
	ctx context.Context,
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	parentID *string,
	summary string,
	firstKeptEntryID string,
	tokensBefore int,
) *database.EntryEntity {
	t.Helper()

	return appendTestCompaction(ctx, t, repository, &database.AppendCompactionInput{
		ParentID:         parentID,
		Details:          nil,
		SessionID:        sessionID,
		Summary:          summary,
		FirstKeptEntryID: firstKeptEntryID,
		TokensBefore:     tokensBefore,
		FromHook:         false,
	})
}

// appendTestMessageAt is a time-aware variant of appendTestMessage so the
// two helpers don't duplicate their message-building logic.
func appendTestMessageAt(
	ctx context.Context,
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	parentID *string,
	role database.Role,
	content string,
	timestamp time.Time,
) *database.EntryEntity {
	t.Helper()

	entry, err := repository.AppendMessage(ctx, sessionID, parentID, &database.MessageEntity{
		Timestamp: timestamp,
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	})
	require.NoError(t, err)

	return entry
}
