// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
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
	cfg             *config.Config
	sessions        *database.SessionRepository
	extensions      runtimeExtensions
	cache           *ResponseCache
	models          *model.Registry
	client          Completer
	logger          *slog.Logger
	skillsCache     *core.SkillsCache
	toolSchemaCache *toolSchemaCache
	agents          *agent.Catalog
	agentTasks      AgentTaskController
	profile         ExecutionProfile
}

// PromptRequest contains one user prompt invocation.
type PromptRequest struct {
	OnEvent        func(StreamEvent)          `json:"-"`
	OnRetry        RetryEventHandler          `json:"-"`
	OnUserEntry    func(PromptUserEntryEvent) `json:"-"`
	ParentEntryID  *string                    `json:"parent_entry_id,omitempty"`
	SessionID      string                     `json:"session_id"`
	CWD            string                     `json:"cwd"`
	Text           string                     `json:"text"`
	Name           string                     `json:"name"`
	ResumeLatest   bool                       `json:"resume_latest,omitempty"`
	HideUserPrompt bool                       `json:"-"`
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
	// StreamEventContextCompactionStart reports that context compaction has started.
	StreamEventContextCompactionStart StreamEventKind = "context_compaction_start"
	// StreamEventContextCompactionDone reports that context compaction completed.
	StreamEventContextCompactionDone StreamEventKind = "context_compaction_done"
	// StreamEventContextCompactionError reports that context compaction failed.
	StreamEventContextCompactionError StreamEventKind = "context_compaction_error"
	// StreamEventUnknown carries unexpected provider events without persistence side effects.
	StreamEventUnknown StreamEventKind = "unknown"
)

// StreamEvent is emitted during prompt execution before final persistence.
type StreamEvent struct {
	ToolCallEvent *ToolCallEvent    `json:"tool_call_event,omitempty"`
	ToolEvent     *ToolEvent        `json:"tool_event,omitempty"`
	Usage         *model.TokenUsage `json:"usage,omitempty"`
	Kind          StreamEventKind   `json:"kind"`
	Text          string            `json:"text,omitempty"`
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
	Config      *config.Config
	Sessions    *database.SessionRepository
	Extensions  runtimeExtensions
	Cache       *ResponseCache
	Models      *model.Registry
	Client      Completer
	Logger      *slog.Logger
	SkillsCache *core.SkillsCache
	Agents      *agent.Catalog
}

// NewRuntime creates an assistant runtime.
func NewRuntime(options *RuntimeOptions) *Runtime {
	if options == nil {
		return nil
	}

	client := options.Client
	if client == nil {
		client = NewHTTPClient()
	}

	return &Runtime{
		cfg:             options.Config,
		sessions:        options.Sessions,
		extensions:      options.Extensions,
		cache:           options.Cache,
		models:          options.Models,
		client:          client,
		logger:          options.Logger,
		skillsCache:     options.SkillsCache,
		toolSchemaCache: newToolSchemaCache(),
		agents:          options.Agents,
		agentTasks:      nil,
		profile:         topLevelExecutionProfile(),
	}
}

// SetAgentTaskController enables asynchronous subagent tools.
// It must be called during application startup, before Prompt is used.
func (runtime *Runtime) SetAgentTaskController(controller AgentTaskController) {
	runtime.agentTasks = controller
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

	persistCtx, persistCancel := promptPersistenceContext(ctx)
	defer persistCancel()

	activeSession, sessionEvent, err := runtime.resolveSession(persistCtx, request)
	if err != nil {
		return nil, err
	}

	runtime.dispatchObservationalLifecycle(ctx, sessionEvent, lifecyclepayload.Session(activeSession))

	userEntry, parentID, err := runtime.appendPromptUserEntry(persistCtx, activeSession, request)
	if err != nil {
		return nil, err
	}

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

func (runtime *Runtime) appendPromptUserEntry(
	ctx context.Context,
	activeSession *database.SessionEntity,
	request *PromptRequest,
) (*database.EntryEntity, *string, error) {
	parentID, err := runtime.promptParentID(ctx, activeSession.ID, request.ParentEntryID)
	if err != nil {
		return nil, nil, err
	}

	userEntry, err := runtime.appendUserPromptEntry(
		ctx,
		activeSession.ID,
		parentID,
		request.Text,
		!request.HideUserPrompt,
	)
	if err != nil {
		return nil, nil, oops.In("assistant").Code("append_user").Wrapf(err, "append user message")
	}

	request.SessionID = activeSession.ID

	runtime.dispatchMessageAppend(ctx, userEntry)
	runtime.notifyPromptUserEntry(request, activeSession.ID, userEntry.ID)

	return userEntry, parentID, nil
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

// AgentDefinitions returns immutable copies of discovered agent profiles.
func (runtime *Runtime) AgentDefinitions() []agent.Definition {
	if runtime == nil || runtime.agents == nil {
		return nil
	}

	return runtime.agents.Definitions()
}

// AgentTasks returns durable agent tasks owned by a session.
func (runtime *Runtime) AgentTasks(
	ctx context.Context,
	ownerSessionID string,
	limit int,
) ([]database.TaskEntity, error) {
	if runtime == nil || runtime.agentTasks == nil {
		return nil, nil
	}

	tasks, err := runtime.agentTasks.List(ctx, ownerSessionID, limit)
	if err != nil {
		return nil, oops.In("assistant").Code("list_agent_tasks").Wrapf(err, "list agent tasks")
	}

	return tasks, nil
}

// AgentTask returns one durable agent task.
func (runtime *Runtime) AgentTask(
	ctx context.Context,
	taskID string,
) (*database.AgentTaskEntity, bool, error) {
	if runtime == nil || runtime.agentTasks == nil {
		return nil, false, nil
	}

	task, found, err := runtime.agentTasks.Get(ctx, taskID)
	if err != nil {
		return nil, false, oops.In("assistant").Code("get_agent_task").Wrapf(err, "get agent task")
	}

	return task, found, nil
}

// SubscribeAgentTask follows persisted events for one agent task.
func (runtime *Runtime) SubscribeAgentTask(
	taskID string,
) (events <-chan database.TaskEventEntity, cancel func()) {
	if runtime == nil || runtime.agentTasks == nil {
		channel := make(chan database.TaskEventEntity)
		close(channel)

		return channel, func() {
			// The closed channel has no live subscription to cancel.
		}
	}

	return runtime.agentTasks.SubscribeAgentTask(taskID)
}

// CancelAgentTask requests cancellation of one durable agent task.
func (runtime *Runtime) CancelAgentTask(
	ctx context.Context,
	ownerSessionID string,
	taskID string,
) (*database.TaskEntity, bool, error) {
	if runtime == nil || runtime.agentTasks == nil {
		return nil, false, nil
	}

	task, found, err := runtime.agentTasks.Cancel(ctx, ownerSessionID, taskID)
	if err != nil {
		return nil, false, oops.In("assistant").Code("cancel_agent_task").Wrapf(err, "cancel agent task")
	}

	return task, found, nil
}

// AgentDiagnostics returns profile discovery and validation diagnostics.
func (runtime *Runtime) AgentDiagnostics() []agent.Diagnostic {
	if runtime == nil || runtime.agents == nil {
		return nil
	}

	return runtime.agents.Diagnostics()
}

// SessionRepository returns the underlying session repository for command and UI layers.
func (runtime *Runtime) SessionRepository() *database.SessionRepository {
	return runtime.sessions
}

// loadSkills returns skills from the runtime cache when available, falling back
// to a direct disk scan. This avoids redundant filesystem I/O on every prompt.
func (runtime *Runtime) loadSkills(cwd string) []core.Skill {
	if runtime.skillsCache != nil {
		return runtime.skillsCache.Get(cwd).Skills
	}

	return core.LoadSkills(cwd, nil, true).Skills
}

// WithExecutionProfile returns a runtime view with an immutable execution profile.
// Runtime dependencies remain shared and safe for concurrent prompt execution.
func (runtime *Runtime) WithExecutionProfile(profile *ExecutionProfile) *Runtime {
	clonedProfile := cloneExecutionProfile(profile)

	return &Runtime{
		cfg: runtime.cfg, sessions: runtime.sessions, extensions: runtime.extensions, cache: runtime.cache,
		models: runtime.models, client: runtime.client, logger: runtime.logger, skillsCache: runtime.skillsCache,
		toolSchemaCache: runtime.toolSchemaCache, agents: runtime.agents,
		agentTasks: runtime.agentTasks, profile: clonedProfile,
	}
}

func (runtime *Runtime) loadAgentInstructions(cwd string) string {
	if runtime.skillsCache != nil {
		return runtime.skillsCache.Get(cwd).AgentInstructions
	}

	return core.LoadAgentInstructions(cwd)
}

// ModelRegistry returns the model registry used by the runtime.
func (runtime *Runtime) ModelRegistry() *model.Registry {
	return runtime.models
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
		return absolutePath(".")
	}

	return absolutePath(cwd)
}

func absolutePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)

	return absolute, assistantError(err, "resolve absolute path")
}
