package assistant

import (
	"context"
	"log/slog"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
)

type promptTurnLifecycle struct {
	ctx         context.Context
	runtime     *Runtime
	sessionID   string
	userEntryID string
	ended       bool
}

func newPromptTurnLifecycle(
	ctx context.Context,
	runtime *Runtime,
	sessionID string,
	userEntryID string,
) *promptTurnLifecycle {
	return &promptTurnLifecycle{
		ctx:         ctx,
		runtime:     runtime,
		sessionID:   sessionID,
		userEntryID: userEntryID,
		ended:       false,
	}
}

func (lifecycle *promptTurnLifecycle) dispatchEnd(
	assistantEntryID string,
	cached bool,
	usage model.TokenUsage,
) {
	if lifecycle == nil || lifecycle.ended {
		return
	}
	lifecycle.ended = true
	lifecycle.runtime.dispatchTurnEndLifecycle(
		lifecycle.ctx,
		lifecycle.sessionID,
		lifecycle.userEntryID,
		assistantEntryID,
		cached,
		usage,
	)
}

func (lifecycle *promptTurnLifecycle) dispatchError(err error) {
	if lifecycle == nil || lifecycle.ended || err == nil {
		return
	}
	lifecycle.ended = true
	lifecycle.runtime.dispatchTurnErrorLifecycle(lifecycle.ctx, lifecycle.sessionID, lifecycle.userEntryID, err)
}

func (runtime *Runtime) dispatchLifecycle(
	ctx context.Context,
	name extension.LifecycleEventName,
	payload map[string]any,
) {
	runtime.emit(ctx, string(name), payload)
	if runtime.extensions == nil {
		return
	}
	_, err := runtime.extensions.DispatchLifecycle(ctx, extension.LifecycleEvent{
		Payload: payload,
		Name:    name,
	})
	if err != nil && runtime.logger != nil {
		runtime.logger.Debug(
			"extension lifecycle event failed",
			slog.String("event", string(name)),
			slog.Any("error", err),
		)
	}
}

func (runtime *Runtime) dispatchMessageAppend(ctx context.Context, entry *database.EntryEntity) {
	if entry == nil {
		return
	}
	runtime.dispatchLifecycle(ctx, extension.LifecycleMessageAppend, entryLifecyclePayload(entry))
}

func (runtime *Runtime) dispatchTurnStartLifecycle(
	ctx context.Context,
	sessionID string,
	request *PromptRequest,
	userEntryID string,
	parentEntryID *string,
) {
	payload := turnLifecyclePayload(sessionID, request.CWD, request.Text, userEntryID, parentEntryID)
	runtime.dispatchLifecycle(ctx, extension.LifecycleBeforeAgentStart, payload)
	runtime.dispatchLifecycle(ctx, extension.LifecycleAgentStart, payload)
	runtime.dispatchLifecycle(ctx, extension.LifecycleTurnStart, payload)
}

func (runtime *Runtime) dispatchTurnEndLifecycle(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	assistantEntryID string,
	cached bool,
	usage model.TokenUsage,
) {
	payload := turnEndLifecyclePayload(sessionID, userEntryID, assistantEntryID, cached, nil, usage)
	runtime.dispatchLifecycle(ctx, extension.LifecycleTurnEnd, payload)
	runtime.dispatchLifecycle(ctx, extension.LifecycleAgentEnd, payload)
}

func (runtime *Runtime) dispatchTurnErrorLifecycle(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	turnErr error,
) {
	if turnErr == nil {
		return
	}
	payload := turnEndLifecyclePayload(sessionID, userEntryID, "", false, turnErr, model.EmptyTokenUsage())
	runtime.dispatchLifecycle(ctx, extension.LifecycleTurnEnd, payload)
	runtime.dispatchLifecycle(ctx, extension.LifecycleAgentEnd, payload)
}

func promptLifecyclePayload(request *PromptRequest) map[string]any {
	if request == nil {
		return map[string]any{}
	}

	return map[string]any{
		lifecycleCWDKey:           request.CWD,
		jsonToolNameKey:           request.Name,
		lifecycleParentEntryIDKey: stringPtrValue(request.ParentEntryID),
		lifecyclePromptKey:        request.Text,
		"resume_latest":           request.ResumeLatest,
		jsonSessionIDKey:          request.SessionID,
	}
}

func sessionLifecyclePayload(session *database.SessionEntity) map[string]any {
	if session == nil {
		return map[string]any{}
	}

	return map[string]any{
		lifecycleCWDKey:           session.CWD,
		lifecycleCreatedAtKey:     session.CreatedAt.Format(timeFormatRFC3339Nano),
		jsonToolNameKey:           session.Name,
		lifecycleParentSessionKey: session.ParentSession,
		jsonSessionIDKey:          session.ID,
		lifecycleUpdatedAtKey:     session.UpdatedAt.Format(timeFormatRFC3339Nano),
	}
}

func turnLifecyclePayload(
	sessionID string,
	cwd string,
	prompt string,
	userEntryID string,
	parentEntryID *string,
) map[string]any {
	return map[string]any{
		lifecycleCWDKey:           cwd,
		lifecycleParentEntryIDKey: stringPtrValue(parentEntryID),
		lifecyclePromptKey:        prompt,
		jsonSessionIDKey:          sessionID,
		lifecycleUserEntryIDKey:   userEntryID,
	}
}

func turnEndLifecyclePayload(
	sessionID string,
	userEntryID string,
	assistantEntryID string,
	cached bool,
	turnErr error,
	usage model.TokenUsage,
) map[string]any {
	payload := map[string]any{
		lifecycleAssistantEntryIDKey: assistantEntryID,
		"cached":                     cached,
		lifecycleErrorKey:            "",
		jsonSessionIDKey:             sessionID,
		"usage":                      tokenUsageLifecyclePayload(usage),
		lifecycleUserEntryIDKey:      userEntryID,
	}
	if turnErr != nil {
		payload[lifecycleErrorKey] = turnErr.Error()
	}

	return payload
}

func entryLifecyclePayload(entry *database.EntryEntity) map[string]any {
	return map[string]any{
		lifecycleCreatedAtKey:            entry.CreatedAt.Format(timeFormatRFC3339Nano),
		"custom_type":                    entry.CustomType,
		"display":                        entry.Display,
		lifecycleEntryIDKey:              entry.ID,
		"entry_type":                     string(entry.Type),
		jsonModelKey:                     entry.Message.Model,
		"model_facing":                   entry.ModelFacing,
		"parent_id":                      stringPtrValue(entry.ParentID),
		"provider":                       entry.Message.Provider,
		jsonRoleKey:                      string(entry.Message.Role),
		jsonSessionIDKey:                 entry.SessionID,
		jsonSummaryKey:                   entry.Summary,
		jsonTextKey:                      entry.Message.Content,
		"token_estimate":                 entry.TokenEstimate,
		"tool_args_json":                 entry.ToolArgsJSON,
		"tool_name":                      entry.ToolName,
		"tool_status":                    entry.ToolStatus,
		"branch_from_entry_id":           entry.BranchFromEntryID,
		"compaction_first_kept_entry_id": entry.CompactionFirstKeptEntryID,
		"compaction_tokens_before":       entry.CompactionTokensBefore,
	}
}

func tokenUsageLifecyclePayload(usage model.TokenUsage) map[string]any {
	return map[string]any{
		jsonContextTokensKey: usage.ContextTokens,
		jsonContextWindowKey: usage.ContextWindow,
		jsonInputTokensKey:   usage.InputTokens,
		jsonOutputTokensKey:  usage.OutputTokens,
	}
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

const (
	lifecycleAssistantEntryIDKey = "assistant_entry_id"
	lifecycleCWDKey              = "cwd"
	lifecycleCreatedAtKey        = "created_at"
	lifecycleEntryIDKey          = "entry_id"
	lifecycleErrorKey            = "error"
	lifecycleParentEntryIDKey    = "parent_entry_id"
	lifecycleParentSessionKey    = "parent_session"
	lifecyclePromptKey           = "prompt"
	lifecycleUpdatedAtKey        = "updated_at"
	lifecycleUserEntryIDKey      = "user_entry_id"
	timeFormatRFC3339Nano        = "2006-01-02T15:04:05.999999999Z07:00"
)
