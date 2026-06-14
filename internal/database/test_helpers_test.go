package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

// sessionTestHelper bundles the common test dependencies so helper
// methods stay under SonarCloud's 7-parameter limit.
type sessionTestHelper struct {
	ctx        context.Context
	t          *testing.T
	repository *database.SessionRepository
}

// appendBranchSummary creates a branch summary entry with sensible
// defaults so call sites don't repeat the 7-argument AppendBranchSummary
// signature.
func (h sessionTestHelper) appendBranchSummary(
	sessionID string,
	parentID *string,
	sourceEntryID string,
	summary string,
) *database.EntryEntity {
	h.t.Helper()

	entry, err := h.repository.AppendBranchSummary(
		h.ctx,
		sessionID,
		parentID,
		sourceEntryID,
		summary,
		nil,
		false,
	)
	require.NoError(h.t, err)

	return entry
}

// appendCompaction creates a compaction entry from a fully-specified
// input so call sites don't repeat the 7-field struct literal.
func (h sessionTestHelper) appendCompaction(
	input *database.AppendCompactionInput,
) *database.EntryEntity {
	h.t.Helper()

	entry, err := h.repository.AppendCompaction(h.ctx, input)
	require.NoError(h.t, err)

	return entry
}

// appendCompactionSimple is a convenience wrapper for the most common
// test compaction shape: a parent, a session, a summary, and a first-kept
// entry, with no details or hook.
func (h sessionTestHelper) appendCompactionSimple(
	sessionID string,
	parentID *string,
	summary string,
	firstKeptEntryID string,
	tokensBefore int,
) *database.EntryEntity {
	h.t.Helper()

	return h.appendCompaction(&database.AppendCompactionInput{
		ParentID:         parentID,
		Details:          nil,
		SessionID:        sessionID,
		Summary:          summary,
		FirstKeptEntryID: firstKeptEntryID,
		TokensBefore:     tokensBefore,
		FromHook:         false,
	})
}

// appendMessage appends a message with the current time.
func (h sessionTestHelper) appendMessage(
	sessionID string,
	parentID *string,
	role database.Role,
	content string,
) *database.EntryEntity {
	h.t.Helper()

	return h.appendMessageAt(sessionID, parentID, role, content, time.Now().UTC())
}

// appendMessageAt is a time-aware variant of appendMessage so the
// two helpers don't duplicate their message-building logic.
func (h sessionTestHelper) appendMessageAt(
	sessionID string,
	parentID *string,
	role database.Role,
	content string,
	timestamp time.Time,
) *database.EntryEntity {
	h.t.Helper()

	entry, err := h.repository.AppendMessage(h.ctx, sessionID, parentID, &database.MessageEntity{
		Timestamp: timestamp,
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	})
	require.NoError(h.t, err)

	return entry
}
