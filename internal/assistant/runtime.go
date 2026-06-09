// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
)

type runtimeExtensions interface {
	extension.CommandRunner
	extension.EventEmitter
	extension.LifecycleDispatcher
	toolProvider
}

// Runtime coordinates prompt handling and durable sessions.
type Runtime struct {
	cfg        *config.Config
	sessions   *database.SessionRepository
	extensions runtimeExtensions
	cache      *ResponseCache
	events     *event.Bus
	models     *model.Registry
	client     CompletionClient
	logger     *slog.Logger
}

// PromptRequest contains one user prompt invocation.
type PromptRequest struct {
	OnEvent       func(StreamEvent)          `json:"-"`
	OnRetry       RetryEventHandler          `json:"-"`
	OnUserEntry   func(PromptUserEntryEvent) `json:"-"`
	ParentEntryID *string                    `json:"parent_entry_id,omitempty"`
	SessionID     string                     `json:"session_id"`
	CWD           string                     `json:"cwd"`
	Text          string                     `json:"text"`
	Name          string                     `json:"name"`
	ResumeLatest  bool                       `json:"resume_latest,omitempty"`
}

// PromptUserEntryEvent identifies the persisted user entry for an active prompt.
type PromptUserEntryEvent struct {
	SessionID string `json:"session_id"`
	EntryID   string `json:"entry_id"`
}

// StreamEventKind identifies incremental assistant activity.
type StreamEventKind string

// RetryEventHandler receives retry lifecycle events.
type RetryEventHandler func(RetryEvent)

const (
	// StreamEventTextDelta carries assistant text as it arrives.
	StreamEventTextDelta StreamEventKind = "text_delta"
	// StreamEventThinkingDelta carries model thinking/reasoning text as it arrives.
	StreamEventThinkingDelta StreamEventKind = "thinking_delta"
	// StreamEventToolStart announces a tool call before execution.
	StreamEventToolStart StreamEventKind = "tool_start"
	// StreamEventToolResult carries the completed tool call result.
	StreamEventToolResult StreamEventKind = "tool_result"
	// StreamEventSkillLoaded carries an explicitly loaded Agent Skill.
	StreamEventSkillLoaded StreamEventKind = "skill_loaded"
	// StreamEventUsage carries estimated or provider-reported token usage.
	StreamEventUsage StreamEventKind = "usage"
	// StreamEventUsageSnapshot carries a fresh full-context usage snapshot that should replace prior UI usage.
	StreamEventUsageSnapshot StreamEventKind = "usage_snapshot"
	// StreamEventContextCompaction carries UI-only context compaction notices.
	StreamEventContextCompaction StreamEventKind = "context_compaction"
	// StreamEventUnknown carries unexpected provider events without persistence side effects.
	StreamEventUnknown StreamEventKind = "unknown"
)

// StreamEvent is emitted during prompt execution before final persistence.
type StreamEvent struct {
	ToolEvent *ToolEvent        `json:"tool_event,omitempty"`
	Usage     *model.TokenUsage `json:"usage,omitempty"`
	Kind      StreamEventKind   `json:"kind"`
	Text      string            `json:"text,omitempty"`
}

// PromptResponse describes persisted prompt output.
type PromptResponse struct {
	SessionID        string           `json:"session_id"`
	UserEntryID      string           `json:"user_entry_id"`
	AssistantEntryID string           `json:"assistant_entry_id"`
	Text             string           `json:"text"`
	Thinking         []string         `json:"thinking,omitempty"`
	ToolEvents       []ToolEvent      `json:"tool_events,omitempty"`
	Usage            model.TokenUsage `json:"usage"`
	Cached           bool             `json:"cached"`
}

type responseBundle struct {
	ParentEntryID *string
	Text          string
	Thinking      []string
	ToolEvents    []ToolEvent
	Usage         model.TokenUsage
	ModelFacing   bool
}

// RuntimeOptions contains dependencies for an assistant runtime.
type RuntimeOptions struct {
	Config     *config.Config
	Sessions   *database.SessionRepository
	Extensions runtimeExtensions
	Cache      *ResponseCache
	Events     *event.Bus
	Models     *model.Registry
	Client     CompletionClient
	Logger     *slog.Logger
}

// NewRuntime creates an assistant runtime.
func NewRuntime(options *RuntimeOptions) *Runtime {
	if options == nil {
		return nil
	}

	client := options.Client
	if client == nil {
		client = NewHTTPCompletionClient()
	}

	return &Runtime{
		cfg:        options.Config,
		sessions:   options.Sessions,
		extensions: options.Extensions,
		cache:      options.Cache,
		events:     options.Events,
		models:     options.Models,
		client:     client,
		logger:     options.Logger,
	}
}

// Prompt appends a user prompt and an assistant response to the selected session.
func (runtime *Runtime) Prompt(ctx context.Context, request *PromptRequest) (response *PromptResponse, err error) {
	if request == nil {
		return nil, oops.In("assistant").Code("nil_prompt_request").Errorf("prompt request is nil")
	}
	promptPayload := lifecyclePromptRequest(request)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleInput, lifecyclepayload.Prompt(promptPayload))
	runtime.dispatchObservationalLifecycle(
		ctx,
		extension.LifecyclePromptPrepare,
		lifecyclepayload.Prompt(promptPayload),
	)

	activeSession, sessionEvent, err := runtime.resolveSession(ctx, request)
	if err != nil {
		return nil, err
	}
	runtime.dispatchObservationalLifecycle(ctx, sessionEvent, lifecyclepayload.Session(activeSession))

	parentID, err := runtime.promptParentID(ctx, activeSession.ID, request.ParentEntryID)
	if err != nil {
		return nil, err
	}

	userEntry, err := runtime.appendUserPromptEntry(ctx, activeSession.ID, parentID, request.Text)
	if err != nil {
		return nil, oops.In("assistant").Code("append_user").Wrapf(err, "append user message")
	}
	request.SessionID = activeSession.ID
	runtime.dispatchMessageAppend(ctx, userEntry)
	runtime.notifyPromptUserEntry(request, activeSession.ID, userEntry.ID)
	turnLifecycle := newPromptTurnLifecycle(runtime, activeSession.ID, userEntry.ID)
	runtime.dispatchTurnStartLifecycle(ctx, activeSession.ID, request, userEntry.ID, parentID)
	defer func() {
		turnLifecycle.dispatchError(ctx, err)
	}()

	bundle, cached, err := runtime.respondWithPartialProgress(ctx, activeSession.ID, userEntry.ID, request)
	if err != nil {
		return nil, err
	}

	compactedBeforeRequest := bundle.ParentEntryID != nil
	assistantParentID := bundle.ParentEntryID
	if assistantParentID == nil {
		assistantParentID = &userEntry.ID
	}
	assistantParentID, err = runtime.appendAssistantSideEffects(ctx, activeSession.ID, assistantParentID, bundle)
	if err != nil {
		return nil, err
	}
	assistantEntry, err := runtime.appendAssistantResponseEntry(ctx, activeSession.ID, assistantParentID, bundle)
	if err != nil {
		return nil, oops.In("assistant").Code("append_assistant").Wrapf(err, "append assistant message")
	}
	runtime.dispatchMessageAppend(ctx, assistantEntry)
	turnLifecycle.dispatchEnd(ctx, assistantEntry.ID, cached, bundle.Usage)
	runtime.maybeAutoCompactAfterResponse(
		ctx,
		activeSession.ID,
		assistantEntry.ID,
		request,
		bundle,
		compactedBeforeRequest,
	)

	return &PromptResponse{
		SessionID:        activeSession.ID,
		UserEntryID:      userEntry.ID,
		AssistantEntryID: assistantEntry.ID,
		Text:             bundle.Text,
		Thinking:         bundle.Thinking,
		ToolEvents:       bundle.ToolEvents,
		Usage:            bundle.Usage,
		Cached:           cached,
	}, nil
}

func (runtime *Runtime) maybeAutoCompactAfterResponse(
	ctx context.Context,
	sessionID string,
	assistantEntryID string,
	request *PromptRequest,
	bundle *responseBundle,
	compactedBeforeRequest bool,
) {
	if compactedBeforeRequest || request == nil || bundle == nil {
		return
	}
	usage, compacted := runtime.autoCompactAfterResponse(ctx, &postResponseAutoCompactionInput{
		onEvent:       request.OnEvent,
		sessionID:     sessionID,
		cwd:           request.CWD,
		parentEntryID: assistantEntryID,
	})
	if compacted {
		bundle.Usage = usage
	}
}

// SessionRepository returns the underlying session repository for command and UI layers.
func (runtime *Runtime) SessionRepository() *database.SessionRepository {
	return runtime.sessions
}

// ModelRegistry returns the model registry used by the runtime.
func (runtime *Runtime) ModelRegistry() *model.Registry {
	return runtime.models
}

// EventBus returns the observational reactive event stream for this runtime.
func (runtime *Runtime) EventBus() *event.Bus {
	return runtime.events
}

func (runtime *Runtime) emit(ctx context.Context, channel string, data any) {
	if runtime.events == nil {
		return
	}

	runtime.events.Emit(ctx, channel, data)
}

func splitSlashCommand(prompt string) (name, args string) {
	trimmedPrompt := strings.TrimSpace(strings.TrimPrefix(prompt, slashPrefix))
	if trimmedPrompt == "" {
		return "", ""
	}
	if after, ok := strings.CutPrefix(trimmedPrompt, "skill:"); ok {
		return "skill", after
	}

	commandName, commandArgs, found := strings.Cut(trimmedPrompt, " ")
	if !found {
		return commandName, ""
	}

	return commandName, strings.TrimSpace(commandArgs)
}

// DefaultCWD returns an absolute working directory for prompt requests.
func DefaultCWD(cwd string) (string, error) {
	if cwd == "" {
		return filepath.Abs(".")
	}

	return filepath.Abs(cwd)
}
