package assistant

import (
	"context"
	"log/slog"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
)

type promptTurnLifecycle struct {
	runtime     *Runtime
	sessionID   string
	userEntryID string
	ended       bool
}

func newPromptTurnLifecycle(
	runtime *Runtime,
	sessionID string,
	userEntryID string,
) *promptTurnLifecycle {
	return &promptTurnLifecycle{
		runtime:     runtime,
		sessionID:   sessionID,
		userEntryID: userEntryID,
		ended:       false,
	}
}

func (lifecycle *promptTurnLifecycle) dispatchEnd(
	ctx context.Context,
	assistantEntryID string,
	cached bool,
	usage model.TokenUsage,
) {
	if lifecycle == nil || lifecycle.ended {
		return
	}
	lifecycle.ended = true
	lifecycle.runtime.dispatchTurnEndLifecycle(
		ctx,
		lifecycle.sessionID,
		lifecycle.userEntryID,
		assistantEntryID,
		cached,
		usage,
	)
}

func (lifecycle *promptTurnLifecycle) dispatchError(ctx context.Context, err error) {
	if lifecycle == nil || lifecycle.ended || err == nil {
		return
	}
	lifecycle.ended = true
	lifecycle.runtime.dispatchTurnErrorLifecycle(ctx, lifecycle.sessionID, lifecycle.userEntryID, err)
}

func (runtime *Runtime) dispatchLifecycle(
	ctx context.Context,
	name extension.LifecycleEventName,
	payload map[string]any,
) (extension.LifecycleDispatchResult, error) {
	runtime.emit(ctx, string(name), payload)
	if runtime.extensions == nil {
		return extension.LifecycleDispatchResult{
			Payload:      cloneAnyMap(payload),
			Name:         string(name),
			Errors:       []string{},
			Duration:     0,
			HandlerCount: 0,
			Consumed:     false,
			Stopped:      false,
		}, nil
	}
	result, err := runtime.extensions.DispatchLifecycle(ctx, extension.LifecycleEvent{
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

	return result, err
}

func (runtime *Runtime) dispatchMessageAppend(ctx context.Context, entry *database.EntryEntity) {
	if entry == nil {
		return
	}
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleMessageAppend, entryLifecyclePayload(entry))
}

func (runtime *Runtime) dispatchTurnStartLifecycle(
	ctx context.Context,
	sessionID string,
	request *PromptRequest,
	userEntryID string,
	parentEntryID *string,
) {
	payload := turnLifecyclePayload(sessionID, request.CWD, request.Text, userEntryID, parentEntryID)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleBeforeAgentStart, payload)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleAgentStart, payload)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleTurnStart, payload)
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
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleTurnEnd, payload)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleAgentEnd, payload)
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
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleTurnEnd, payload)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleAgentEnd, payload)
}

func (runtime *Runtime) dispatchObservationalLifecycle(
	ctx context.Context,
	name extension.LifecycleEventName,
	payload map[string]any,
) {
	if _, err := runtime.dispatchLifecycle(ctx, name, payload); err != nil && runtime.logger != nil {
		runtime.logger.Debug(
			"observational lifecycle dispatch failed",
			slog.String("event", string(name)),
			slog.Any("error", err),
		)
	}
}

func (runtime *Runtime) dispatchContextBuild(
	ctx context.Context,
	sessionID string,
	cwd string,
	base *modelContextBase,
	result *contextBuildResult,
) (extension.LifecycleDispatchResult, error) {
	payload := contextBuildLifecyclePayload(sessionID, cwd, base, result)
	return runtime.dispatchLifecycle(ctx, extension.LifecycleContextBuild, payload)
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
		jsonUsageKey:                 tokenUsageLifecyclePayload(usage),
		lifecycleUserEntryIDKey:      userEntryID,
	}
	if turnErr != nil {
		payload[lifecycleErrorKey] = turnErr.Error()
	}

	return payload
}

func contextBuildLifecyclePayload(
	sessionID string,
	cwd string,
	base *modelContextBase,
	result *contextBuildResult,
) map[string]any {
	return map[string]any{
		lifecycleCWDKey:           cwd,
		jsonSessionIDKey:          sessionID,
		"message_count":           len(base.Messages),
		"breakdown":               cloneIntMap(result.Breakdown),
		"contributions":           []any{},
		"max_contribution_tokens": contextContributionMaxTokens,
		"system_tokens":           base.SystemTokens,
		"skill_tokens":            base.SkillTokens,
		"message_tokens":          base.HistoryTokens,
		jsonUsageKey:              tokenUsageLifecyclePayload(result.Usage),
		"model_facing_roles":      modelFacingRoleCounts(base.Messages),
	}
}

func cloneAnyMap(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}

func cloneIntMap(values map[string]int) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}

func estimateMessageTokens(messages []database.MessageEntity) int {
	tokens := 0
	for index := range messages {
		tokens += estimateTokens(messages[index].Content)
	}

	return tokens
}

func modelFacingRoleCounts(messages []database.MessageEntity) map[string]int {
	counts := map[string]int{}
	for index := range messages {
		role := string(messages[index].Role)
		counts[role]++
	}

	return counts
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
