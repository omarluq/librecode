// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
)

func (runtime *Runtime) resolveSession(
	ctx context.Context,
	request *PromptRequest,
) (*database.SessionEntity, extension.LifecycleEventName, error) {
	if request.SessionID != "" {
		return runtime.resolveRequestedSession(ctx, request)
	}

	if request.ResumeLatest {
		return runtime.resolveLatestOrNewSession(ctx, request)
	}

	return runtime.createPromptSession(ctx, request)
}

func (runtime *Runtime) resolveRequestedSession(
	ctx context.Context,
	request *PromptRequest,
) (*database.SessionEntity, extension.LifecycleEventName, error) {
	if request.ResumeLatest {
		return nil, "", oops.
			In("assistant").
			Code("session_selection_conflict").
			Errorf("resume latest cannot be used with an explicit session")
	}

	loadedSession, found, err := runtime.sessions.GetSession(ctx, request.SessionID)
	if err != nil {
		return nil, "", oops.
			In("assistant").
			Code("load_session").
			With("session_id", request.SessionID).
			Wrapf(err, "load requested session")
	}

	if !found {
		return nil, "", oops.
			In("assistant").
			Code("session_not_found").
			With("session_id", request.SessionID).
			Errorf("session not found")
	}

	return loadedSession, extension.LifecycleSessionLoad, nil
}

func (runtime *Runtime) resolveLatestOrNewSession(
	ctx context.Context,
	request *PromptRequest,
) (*database.SessionEntity, extension.LifecycleEventName, error) {
	if request.Name != "" {
		return nil, "", oops.
			In("assistant").
			Code("session_selection_conflict").
			Errorf("resume latest cannot be used with a new session name")
	}

	latestSession, found, err := runtime.sessions.LatestSession(ctx, request.CWD)
	if err != nil {
		return nil, "", oops.
			In("assistant").
			Code("load_latest_session").
			With("cwd", request.CWD).
			Wrapf(err, "load latest session")
	}

	if found {
		return latestSession, extension.LifecycleSessionLoad, nil
	}

	return runtime.createPromptSession(ctx, request)
}

func (runtime *Runtime) createPromptSession(
	ctx context.Context,
	request *PromptRequest,
) (*database.SessionEntity, extension.LifecycleEventName, error) {
	if request.Name != "" {
		session, err := runtime.sessions.CreateSession(ctx, request.CWD, request.Name, "")
		if err != nil {
			return nil, "", oops.
				In("assistant").
				Code("create_named_session").
				With("cwd", request.CWD).
				With("name", request.Name).
				Wrapf(err, "create named session")
		}

		return session, extension.LifecycleSessionStart, nil
	}

	session, err := runtime.sessions.CreateSession(ctx, request.CWD, "", "")
	if err != nil {
		return nil, "", oops.
			In("assistant").
			Code("create_session").
			With("cwd", request.CWD).
			Wrapf(err, "create session")
	}

	return session, extension.LifecycleSessionStart, nil
}

func (runtime *Runtime) notifyPromptUserEntry(request *PromptRequest, sessionID, entryID string) {
	if request.OnUserEntry == nil {
		return
	}

	request.OnUserEntry(PromptUserEntryEvent{SessionID: sessionID, EntryID: entryID})
}

// promptParentID resolves the explicit branch endpoint the prompt is submitted
// against. Explicit endpoints are validated against the session so a cross-session
// or deleted entry cannot silently truncate reconstructed lineage.
func (runtime *Runtime) promptParentID(ctx context.Context, sessionID string, explicitParent *string) (*string, error) {
	if explicitParent != nil && *explicitParent != "" {
		return runtime.explicitPromptParentID(ctx, sessionID, explicitParent)
	}

	leaf, _, err := runtime.sessions.LeafEntry(ctx, sessionID)
	if err != nil {
		return nil, assistantError(err, "load session leaf")
	}

	return parentIDFromEntry(leaf), nil
}

func (runtime *Runtime) explicitPromptParentID(
	ctx context.Context,
	sessionID string,
	explicitParent *string,
) (*string, error) {
	// Validation must read the parent in the same session: a cross-session or
	// missing entry would otherwise append onto a detached root and rebuild a
	// truncated branch without any error.
	_, found, err := runtime.sessions.Entry(ctx, sessionID, *explicitParent)
	if err != nil {
		return nil, oops.In("assistant").
			Code("load_prompt_parent").
			With("session_id", sessionID).
			Wrapf(err, "load prompt parent entry")
	}

	if !found {
		return nil, oops.In("assistant").
			Code("prompt_parent_not_found").
			With("session_id", sessionID).
			With("parent_entry_id", *explicitParent).
			Errorf("prompt parent entry %q is not in session %q", *explicitParent, sessionID)
	}

	return explicitParent, nil
}

func parentIDFromEntry(entry *database.EntryEntity) *string {
	if entry == nil {
		return nil
	}

	return &entry.ID
}
