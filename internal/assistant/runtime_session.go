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

func (runtime *Runtime) promptParentID(ctx context.Context, sessionID string, explicitParent *string) (*string, error) {
	if explicitParent != nil {
		return explicitPromptParentID(explicitParent), nil
	}

	leaf, _, err := runtime.sessions.LeafEntry(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return parentIDFromEntry(leaf), nil
}

func explicitPromptParentID(explicitParent *string) *string {
	if *explicitParent == "" {
		return nil
	}

	return explicitParent
}

func parentIDFromEntry(entry *database.EntryEntity) *string {
	if entry == nil {
		return nil
	}

	return &entry.ID
}
