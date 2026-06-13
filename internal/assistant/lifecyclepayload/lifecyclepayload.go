// Package lifecyclepayload builds extension-facing lifecycle event payloads.
package lifecyclepayload

import (
	"maps"
	"time"

	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/mapsutil"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/units"
)

// Lifecycle payload keys define the stable extension-facing lifecycle payload schema.
const (
	APIKey                = "api"
	AssistantEntryIDKey   = "assistant_entry_id"
	AttemptKey            = "attempt"
	BreakdownKey          = "breakdown"
	ContextTokensKey      = "context_tokens"
	ContextWindowKey      = "context_window"
	CreatedAtKey          = "created_at"
	CWDKey                = "cwd"
	DurationMsKey         = "duration_ms"
	EntryIDKey            = "entry_id"
	ErrorKey              = "error"
	ErrorsKey             = "hook_errors"
	HookCountKey          = "hook_count"
	InputTokensKey        = "input_tokens"
	ModelKey              = "model"
	OutputTokensKey       = "output_tokens"
	ParentEntryIDKey      = "parent_entry_id"
	ParentSessionKey      = "parent_session"
	PhaseKey              = "phase"
	PromptKey             = "prompt"
	ProviderKey           = "provider"
	ProviderPayloadKey    = "payload"
	ProviderHeadersKey    = "headers"
	RoleKey               = "role"
	SessionIDKey          = "session_id"
	SummaryKey            = "summary"
	TextKey               = "text"
	TokensBeforeKey       = "tokens_before"
	ToolErrorKey          = "error"
	ToolNameKey           = "name"
	UpdatedAtKey          = "updated_at"
	UsageKey              = "usage"
	UserEntryIDKey        = "user_entry_id"
	TimeFormatRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
)

// PromptRequest is the lifecycle payload view of an assistant prompt request.
type PromptRequest struct {
	ParentEntryID *string
	CWD           string
	Name          string
	SessionID     string
	Text          string
	ResumeLatest  bool
}

// TurnEnd describes the payload for a finished prompt turn.
type TurnEnd struct {
	Err              error
	AssistantEntryID string
	SessionID        string
	UserEntryID      string
	Usage            model.TokenUsage
	Cached           bool
}

// ProviderRequest describes before-provider-request lifecycle payload metadata.
type ProviderRequest struct {
	Payload       map[string]any
	Headers       map[string]string
	API           string
	ModelID       string
	Provider      string
	SessionID     string
	ThinkingLevel string
	Attempt       int
}

// ProviderResponse describes after-provider-response lifecycle payload metadata.
type ProviderResponse struct {
	FinishReason   llm.FinishReason
	API            string
	ModelID        string
	Provider       string
	SessionID      string
	Text           string
	Usage          model.TokenUsage
	Attempt        int
	ThinkingCount  int
	ToolEventCount int
}

// ProviderError describes provider-error lifecycle payload metadata.
type ProviderError struct {
	Err       error
	API       string
	ModelID   string
	Provider  string
	SessionID string
	Attempt   int
}

// ToolCall describes a tool-call lifecycle payload.
type ToolCall struct {
	Arguments     map[string]any
	ID            string
	Name          string
	ArgumentsJSON string
}

// ToolResult describes a tool-result lifecycle payload.
type ToolResult struct {
	Name          string
	ArgumentsJSON string
	DetailsJSON   string
	Result        string
	Error         string
	IsError       bool
}

// CompactionSaved describes an after-compaction lifecycle payload.
type CompactionSaved struct {
	Entry     *database.EntryEntity
	Plan      *compaction.Plan
	SessionID string
	CWD       string
	Source    string
}

// Prompt builds input and prompt-prepare payloads.
func Prompt(request *PromptRequest) map[string]any {
	if request == nil {
		return map[string]any{}
	}

	return map[string]any{
		CWDKey:           request.CWD,
		ToolNameKey:      request.Name,
		ParentEntryIDKey: stringPtrValue(request.ParentEntryID),
		PromptKey:        request.Text,
		"resume_latest":  request.ResumeLatest,
		SessionIDKey:     request.SessionID,
	}
}

// Session builds session lifecycle payloads.
func Session(session *database.SessionEntity) map[string]any {
	if session == nil {
		return map[string]any{}
	}

	return map[string]any{
		CWDKey:           session.CWD,
		CreatedAtKey:     session.CreatedAt.Format(TimeFormatRFC3339Nano),
		ToolNameKey:      session.Name,
		ParentSessionKey: session.ParentSession,
		SessionIDKey:     session.ID,
		UpdatedAtKey:     session.UpdatedAt.Format(TimeFormatRFC3339Nano),
	}
}

// TurnStart builds turn-start lifecycle payloads.
func TurnStart(sessionID, cwd, prompt, userEntryID string, parentEntryID *string) map[string]any {
	return map[string]any{
		CWDKey:           cwd,
		ParentEntryIDKey: stringPtrValue(parentEntryID),
		PromptKey:        prompt,
		SessionIDKey:     sessionID,
		UserEntryIDKey:   userEntryID,
	}
}

// TurnEndPayload builds turn-end and turn-error lifecycle payloads.
func TurnEndPayload(turn *TurnEnd) map[string]any {
	if turn == nil {
		return map[string]any{}
	}

	payload := map[string]any{
		AssistantEntryIDKey: turn.AssistantEntryID,
		"cached":            turn.Cached,
		ErrorKey:            "",
		SessionIDKey:        turn.SessionID,
		UsageKey:            TokenUsage(turn.Usage),
		UserEntryIDKey:      turn.UserEntryID,
	}
	if turn.Err != nil {
		payload[ErrorKey] = turn.Err.Error()
	}

	return payload
}

// ContextBuild builds context-build lifecycle payloads.
func ContextBuild(sessionID, cwd string, base *contextwindow.Base, result *contextwindow.BuildResult) map[string]any {
	return map[string]any{
		CWDKey:                    cwd,
		SessionIDKey:              sessionID,
		"message_count":           len(base.Messages),
		BreakdownKey:              mapsutil.IntMapToAnyMap(result.Breakdown),
		"contributions":           []any{},
		"topContributors":         TokenContributors(result.Usage.TopContributors),
		"max_contribution_tokens": contextwindow.ContributionMaxTokens,
		"system_tokens":           base.SystemTokens,
		"skill_tokens":            base.SkillTokens,
		"message_tokens":          base.HistoryTokens,
		UsageKey:                  TokenUsage(result.Usage),
		"model_facing_roles":      ModelFacingRoleCounts(base.Messages),
	}
}

// ModelFacingRoleCounts counts model-facing roles for lifecycle diagnostics.
func ModelFacingRoleCounts(messages []database.MessageEntity) map[string]int {
	counts := map[string]int{}

	for index := range messages {
		role := string(messages[index].Role)
		counts[role]++
	}

	return counts
}

// Entry builds message-append lifecycle payloads.
func Entry(entry *database.EntryEntity) map[string]any {
	return map[string]any{
		CreatedAtKey:                     entry.CreatedAt.Format(TimeFormatRFC3339Nano),
		"custom_type":                    entry.CustomType,
		"display":                        entry.Display,
		EntryIDKey:                       entry.ID,
		"entry_type":                     string(entry.Type),
		ModelKey:                         entry.Message.Model,
		"model_facing":                   entry.ModelFacing,
		"parent_id":                      stringPtrValue(entry.ParentID),
		"provider":                       entry.Message.Provider,
		RoleKey:                          string(entry.Message.Role),
		SessionIDKey:                     entry.SessionID,
		SummaryKey:                       entry.Summary,
		TextKey:                          entry.Message.Content,
		"token_estimate":                 entry.TokenEstimate,
		"tool_args_json":                 entry.ToolArgsJSON,
		"tool_name":                      entry.ToolName,
		"tool_status":                    entry.ToolStatus,
		"branch_from_entry_id":           entry.BranchFromEntryID,
		"compaction_first_kept_entry_id": entry.CompactionFirstKeptEntryID,
		"compaction_tokens_before":       entry.CompactionTokensBefore,
	}
}

// TokenUsage builds a lifecycle payload for token usage.
func TokenUsage(usage model.TokenUsage) map[string]any {
	return map[string]any{
		BreakdownKey:      mapsutil.IntMapToAnyMap(usage.Breakdown),
		"topContributors": TokenContributors(usage.TopContributors),
		ContextTokensKey:  usage.ContextTokens,
		ContextWindowKey:  usage.ContextWindow,
		InputTokensKey:    usage.InputTokens,
		OutputTokensKey:   usage.OutputTokens,
	}
}

// TokenContributors builds token-contributor lifecycle payloads.
func TokenContributors(contributors []model.TokenContributor) []any {
	return lo.Map(contributors, func(contributor model.TokenContributor, _ int) any {
		return map[string]any{
			"label":   contributor.Label,
			RoleKey:   contributor.Role,
			"preview": contributor.Preview,
			"tokens":  contributor.Tokens,
			"chars":   contributor.Chars,
		}
	})
}

// ProviderRequestPayload builds before-provider-request lifecycle payloads.
func ProviderRequestPayload(request *ProviderRequest) map[string]any {
	if request == nil {
		return map[string]any{}
	}

	return map[string]any{
		APIKey:             request.API,
		AttemptKey:         request.Attempt,
		ProviderHeadersKey: request.Headers,
		ModelKey:           request.ModelID,
		ProviderKey:        request.Provider,
		SessionIDKey:       request.SessionID,
		ProviderPayloadKey: mapsutil.CloneOrEmpty(request.Payload),
		"thinking_level":   request.ThinkingLevel,
	}
}

// ProviderResponsePayload builds after-provider-response lifecycle payloads.
func ProviderResponsePayload(response *ProviderResponse) map[string]any {
	if response == nil {
		return map[string]any{}
	}

	return map[string]any{
		APIKey:             response.API,
		AttemptKey:         response.Attempt,
		ModelKey:           response.ModelID,
		ProviderKey:        response.Provider,
		SessionIDKey:       response.SessionID,
		TextKey:            response.Text,
		"finish_reason":    string(response.FinishReason),
		"thinking_count":   response.ThinkingCount,
		"tool_event_count": response.ToolEventCount,
		UsageKey:           TokenUsage(response.Usage),
	}
}

// ProviderErrorPayload builds provider-error lifecycle payloads.
func ProviderErrorPayload(providerErr *ProviderError) map[string]any {
	if providerErr == nil {
		return map[string]any{}
	}

	payload := map[string]any{
		APIKey:       providerErr.API,
		AttemptKey:   providerErr.Attempt,
		ModelKey:     providerErr.ModelID,
		ProviderKey:  providerErr.Provider,
		SessionIDKey: providerErr.SessionID,
		ErrorKey:     "",
	}
	if providerErr.Err != nil {
		payload[ErrorKey] = providerErr.Err.Error()
	}

	return payload
}

// ToolCallPayload builds tool-call lifecycle payloads.
func ToolCallPayload(call ToolCall) map[string]any {
	return map[string]any{
		"call_id":        call.ID,
		ToolNameKey:      call.Name,
		"arguments_json": call.ArgumentsJSON,
		"arguments":      call.Arguments,
	}
}

// ToolResultPayload builds tool-result lifecycle payloads.
func ToolResultPayload(event *ToolResult) map[string]any {
	if event == nil {
		return map[string]any{}
	}

	return map[string]any{
		ToolNameKey:      event.Name,
		"arguments_json": event.ArgumentsJSON,
		"details_json":   event.DetailsJSON,
		"is_error":       event.IsError,
		"result":         event.Result,
		ToolErrorKey:     event.Error,
	}
}

// CompactionPreparation builds before-compaction lifecycle payloads.
func CompactionPreparation(sessionID, cwd string, plan *compaction.Plan) map[string]any {
	payload := map[string]any{
		CWDKey:                       cwd,
		SessionIDKey:                 sessionID,
		"first_kept_entry_id":        "",
		TokensBeforeKey:              0,
		"summary_message_count":      0,
		"summarized_entry_ids":       []any{},
		"kept_entry_ids":             []any{},
		"split_turn_summary":         "",
		compaction.FileOperationsKey: []any{},
	}
	if plan == nil {
		return payload
	}

	payload["first_kept_entry_id"] = plan.FirstKeptEntryID
	payload[TokensBeforeKey] = plan.TokensBefore
	payload["summary_message_count"] = len(plan.Messages)
	payload["summarized_entry_ids"] = StringSlice(plan.SummarizedEntryIDs)
	payload["kept_entry_ids"] = StringSlice(plan.KeptEntryIDs)
	payload["split_turn_summary"] = plan.SplitTurnSummary
	payload[compaction.FileOperationsKey] = CompactionFileOperations(plan.FileOperations)

	return payload
}

// CompactionSavedPayload builds after-compaction lifecycle payloads.
func CompactionSavedPayload(saved CompactionSaved) map[string]any {
	payload := CompactionPreparation(saved.SessionID, saved.CWD, saved.Plan)
	payload[EntryIDKey] = ""
	payload[SummaryKey] = ""

	payload["source"] = saved.Source
	if saved.Entry != nil {
		payload[EntryIDKey] = saved.Entry.ID
		payload[SummaryKey] = saved.Entry.Summary
	}

	return payload
}

// CompactionDiagnostics builds compaction lifecycle diagnostic payloads.
func CompactionDiagnostics(plan *compaction.Plan, phase string) map[string]any {
	if plan == nil {
		return map[string]any{PhaseKey: phase}
	}

	return map[string]any{
		PhaseKey:                 phase,
		"summarized_entries":     len(plan.SummarizedEntryIDs),
		"kept_entries":           len(plan.KeptEntryIDs),
		"file_operation_count":   len(plan.FileOperations),
		"has_split_turn_summary": plan.SplitTurnSummary != "",
		TokensBeforeKey:          plan.TokensBefore,
		"first_kept_entry_id":    plan.FirstKeptEntryID,
	}
}

// CompactionFileOperations builds compaction file-operation lifecycle payloads.
func CompactionFileOperations(operations []compaction.FileOperation) []any {
	return lo.Map(operations, func(operation compaction.FileOperation, _ int) any {
		return map[string]any{
			"entry_id": operation.EntryID,
			"action":   operation.Action,
			"path":     operation.Path,
			"tool":     operation.Tool,
			"command":  operation.Command,
		}
	})
}

// Diagnostic builds a lifecycle diagnostic payload.
func Diagnostic(
	event string,
	hookCount int,
	duration time.Duration,
	hookErrors []string,
	extra map[string]any,
) map[string]any {
	payload := map[string]any{
		"event":       event,
		HookCountKey:  hookCount,
		DurationMsKey: DurationMilliseconds(duration),
	}
	if len(hookErrors) > 0 {
		payload[ErrorsKey] = append([]string{}, hookErrors...)
	}

	maps.Copy(payload, extra)

	return payload
}

// DurationMilliseconds converts a duration to millisecond precision for lifecycle payloads.
func DurationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / units.TokenThousand
}

// StringSlice converts a string slice to an extension-friendly any slice.
func StringSlice(values []string) []any {
	return lo.Map(values, func(value string, _ int) any {
		return value
	})
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
