// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/tooltask"
)

type runtimeExtensions interface {
	extension.CommandRunner
	extension.EventEmitter
	extension.LifecycleDispatcher
	toolProvider
}

// Runtime coordinates prompt handling and durable sessions.
type Runtime struct {
	client            Completer
	toolTasks         ToolTaskController
	extensions        runtimeExtensions
	workflowSubmitter WorkflowSubmitter
	agentTasks        AgentTaskController
	models            *model.Registry
	sessions          *database.SessionRepository
	skillsCache       *core.SkillsCache
	toolSchemaCache   *toolSchemaCache
	agents            *agent.Catalog
	cfg               *config.Config
	cache             *ResponseCache
	logger            *slog.Logger
	toolCoordinator   *tool.Coordinator
	attachments       *foregroundAttachments
	newCompactionUUID func() (uuid.UUID, error)
	operations        *sessionOperationCoordinator
	steering          *steeringInboxRegistry
	profile           ExecutionProfile
}

// ImageAttachment is one provider-neutral image supplied with a prompt.
type ImageAttachment struct {
	Name     string `json:"name,omitempty"`
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"-"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// PromptRequest contains one user prompt invocation.
type PromptRequest struct {
	OnEvent          func(StreamEvent)          `json:"-"`
	OnRetry          RetryEventHandler          `json:"-"`
	OnUserEntry      func(PromptUserEntryEvent) `json:"-"`
	OnSteeringReturn func([]SteeringMessage)    `json:"-"`
	ParentEntryID    *string                    `json:"parent_entry_id,omitempty"`
	SessionID        string                     `json:"session_id"`
	CWD              string                     `json:"cwd"`
	Text             string                     `json:"text"`
	Name             string                     `json:"name"`
	Images           []ImageAttachment          `json:"images,omitempty"`
	ResumeLatest     bool                       `json:"resume_latest,omitempty"`
	HideUserPrompt   bool                       `json:"-"`
}

// PromptUserEntryEvent identifies the persisted user entry for an active prompt.
type PromptUserEntryEvent struct {
	SessionID string `json:"session_id"`
	EntryID   string `json:"entry_id"`
}

// SteeringRequest targets the active run identified by its initial user entry.
type SteeringRequest struct {
	SessionID      string            `json:"session_id"`
	RunID          string            `json:"run_id"`
	Text           string            `json:"text"`
	Images         []ImageAttachment `json:"images,omitempty"`
	HideUserPrompt bool              `json:"-"`
}

// SteeringConsumedEvent identifies a steering message after it becomes durable.
type SteeringConsumedEvent struct {
	EntryID        string            `json:"entry_id"`
	Text           string            `json:"text"`
	Images         []ImageAttachment `json:"images,omitempty"`
	HideUserPrompt bool              `json:"hide_user_prompt"`
}

// SteeringMessage is a user draft returned when a run settles before consumption.
type SteeringMessage struct {
	Text           string            `json:"text"`
	Images         []ImageAttachment `json:"images,omitempty"`
	HideUserPrompt bool              `json:"-"`
}

// Steer transfers one user message to an active run's steering inbox.
func (runtime *Runtime) Steer(ctx context.Context, request *SteeringRequest) error {
	if err := ctx.Err(); err != nil {
		return oops.In("assistant").Code("steering_canceled").Wrapf(err, "accept steering message")
	}

	if runtime == nil || runtime.steering == nil {
		return ErrSteeringInactive
	}

	if request == nil {
		return oops.In("assistant").Code("nil_steering_request").
			Wrapf(ErrSteeringInvalidInput, "steering request is nil")
	}

	if strings.TrimSpace(request.SessionID) == "" {
		return oops.In("assistant").Code("steering_session_required").
			Wrapf(ErrSteeringInvalidInput, "steering session ID is required")
	}

	if strings.TrimSpace(request.RunID) == "" {
		return oops.In("assistant").Code("steering_run_required").
			Wrapf(ErrSteeringInvalidInput, "steering run ID is required")
	}

	prompt := &PromptRequest{
		OnEvent: nil, OnRetry: nil, OnUserEntry: nil, OnSteeringReturn: nil, ParentEntryID: nil,
		SessionID: "", CWD: "", Text: request.Text, Images: request.Images, Name: "",
		ResumeLatest: false, HideUserPrompt: false,
	}
	if _, err := runtime.preparePromptRequest(prompt); err != nil {
		return errors.Join(ErrSteeringInvalidInput, err)
	}

	return runtime.steering.accept(request.SessionID, request.RunID, steeringDraft{
		Text: request.Text, Images: request.Images, HideUserPrompt: request.HideUserPrompt,
	})
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
	// StreamEventUsageTotal carries cumulative provider-reported usage for one run.
	StreamEventUsageTotal StreamEventKind = "usage_total"
	// StreamEventSteeringConsumed reports that a steering message became durable.
	StreamEventSteeringConsumed StreamEventKind = "steering_consumed"
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
	Text          string
	Thinking      []string
	ToolEvents    []ToolEvent
	Usage         model.TokenUsage
	ProviderUsage model.TokenUsage
	ModelFacing   bool
}

// RuntimeOptions contains dependencies for an assistant runtime.
type RuntimeOptions struct {
	Config            *config.Config
	Sessions          *database.SessionRepository
	Extensions        runtimeExtensions
	Cache             *ResponseCache
	Models            *model.Registry
	Client            Completer
	Logger            *slog.Logger
	SkillsCache       *core.SkillsCache
	Agents            *agent.Catalog
	AgentTasks        AgentTaskController
	WorkflowSubmitter WorkflowSubmitter
	ToolTasks         ToolTaskController
	ToolCoordinator   *tool.Coordinator
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
		cfg:               options.Config,
		sessions:          options.Sessions,
		extensions:        options.Extensions,
		cache:             options.Cache,
		models:            options.Models,
		client:            client,
		logger:            options.Logger,
		skillsCache:       options.SkillsCache,
		toolSchemaCache:   newToolSchemaCache(),
		agents:            options.Agents,
		agentTasks:        options.AgentTasks,
		workflowSubmitter: options.WorkflowSubmitter,
		toolTasks:         options.ToolTasks,
		toolCoordinator:   options.ToolCoordinator,
		attachments:       newForegroundAttachments(),
		operations:        newSessionOperationCoordinator(),
		steering:          newSteeringInboxRegistry(defaultSteeringInboxCapacity),
		newCompactionUUID: uuid.NewV7,
		profile:           topLevelExecutionProfile(),
	}
}

func (runtime *Runtime) acquirePromptOperation(ctx context.Context, sessionID string) (func(), error) {
	release, err := runtime.operations.acquire(ctx, sessionID)
	if err != nil {
		return nil, oops.In("assistant").Code("prompt_operation_wait").Wrapf(err, "wait for session operation")
	}

	return release, nil
}

// Prompt appends a user prompt and an assistant response to the selected session.
func (runtime *Runtime) Prompt(ctx context.Context, request *PromptRequest) (response *PromptResponse, err error) {
	originalRequest, request, err := runtime.validateAndPreparePromptRequest(request)
	if err != nil {
		return nil, err
	}

	runtime.dispatchPromptInputLifecycle(ctx, request)

	activeSession, sessionEvent, err := runtime.resolveSessionWithPersistence(ctx, request)
	if err != nil {
		return nil, err
	}

	originalRequest.SessionID = activeSession.ID

	releaseOperation, err := runtime.acquirePromptOperation(ctx, activeSession.ID)
	if err != nil {
		return nil, err
	}
	defer releaseOperation()

	runtime.dispatchObservationalLifecycle(ctx, sessionEvent, lifecyclepayload.Session(activeSession))

	userEntry, parentID, err := runtime.appendPromptUserEntryWithPersistence(ctx, activeSession, request)
	if err != nil {
		return nil, err
	}

	turnLifecycle := newPromptTurnLifecycle(runtime, activeSession.ID, userEntry.ID)

	runtime.dispatchTurnStartLifecycle(ctx, activeSession.ID, request, userEntry.ID, parentID)
	defer func() {
		turnLifecycle.dispatchError(ctx, err)
	}()

	lineage := newPromptLineageWithEvents(userEntry.ID, request.OnEvent)

	if registerErr := runtime.registerSteeringInbox(activeSession.ID, userEntry.ID); registerErr != nil {
		return nil, registerErr
	}

	steeringOpen := runtime.steering != nil
	defer func() {
		runtime.closePromptSteering(activeSession.ID, userEntry.ID, request, steeringOpen)
	}()

	bundle, cached, err := runtime.respondWithPartialProgress(ctx, activeSession.ID, lineage, request)
	if err != nil {
		return nil, err
	}

	compactedBeforeRequest := lineage.activeParentEntryID != userEntry.ID

	assistantEntry, err := runtime.persistAssistantBundleWithPersistence(ctx, activeSession.ID, lineage, bundle)
	if err != nil {
		return nil, err
	}

	// Stop accepting steering once the response is durable and before lifecycle work.
	steeringOpen = runtime.closePromptSteering(activeSession.ID, userEntry.ID, request, steeringOpen)

	runtime.dispatchMessageAppend(ctx, assistantEntry)
	turnLifecycle.dispatchEnd(ctx, assistantEntry.ID, cached, &bundle.Usage)
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

func (runtime *Runtime) closePromptSteering(
	sessionID string,
	entryID string,
	request *PromptRequest,
	open bool,
) bool {
	if open {
		runtime.closeSteeringInbox(sessionID, entryID, request.OnSteeringReturn)
	}

	return false
}

func (runtime *Runtime) validateAndPreparePromptRequest(
	request *PromptRequest,
) (original, prepared *PromptRequest, err error) {
	if request == nil {
		return nil, nil, oops.In("assistant").Code("nil_prompt_request").Errorf("prompt request is nil")
	}

	prepared, err = runtime.preparePromptRequest(request)

	return request, prepared, err
}

func (runtime *Runtime) registerSteeringInbox(sessionID, runID string) error {
	if runtime.steering == nil {
		return nil
	}

	if err := runtime.steering.register(sessionID, runID); err != nil {
		return oops.In("assistant").Code("steering_register").Wrapf(err, "register active steering inbox")
	}

	return nil
}

func (runtime *Runtime) closeSteeringInbox(
	sessionID string,
	runID string,
	onReturn func([]SteeringMessage),
) {
	drafts, err := runtime.steering.close(sessionID, runID)
	if err != nil || len(drafts) == 0 || onReturn == nil {
		return
	}

	messages := make([]SteeringMessage, len(drafts))
	for index := range drafts {
		messages[index] = SteeringMessage{
			Text: drafts[index].Text, Images: drafts[index].Images,
			HideUserPrompt: drafts[index].HideUserPrompt,
		}
	}

	onReturn(messages)
}

func (runtime *Runtime) resolveSessionWithPersistence(
	ctx context.Context,
	request *PromptRequest,
) (*database.SessionEntity, extension.LifecycleEventName, error) {
	persistCtx, cancel := runtime.promptPersistenceContext(ctx, 1)
	defer cancel()

	return runtime.resolveSession(persistCtx, request)
}

func (runtime *Runtime) appendPromptUserEntryWithPersistence(
	ctx context.Context,
	activeSession *database.SessionEntity,
	request *PromptRequest,
) (*database.EntryEntity, *string, error) {
	persistCtx, cancel := runtime.promptPersistenceContext(ctx, 1)
	defer cancel()

	return runtime.appendPromptUserEntry(persistCtx, activeSession, request)
}

func (runtime *Runtime) dispatchPromptInputLifecycle(ctx context.Context, request *PromptRequest) {
	payload := lifecyclepayload.Prompt(lifecyclePromptRequest(request))
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleInput, payload)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecyclePromptPrepare, payload)
}

func (runtime *Runtime) persistAssistantBundleWithPersistence(
	ctx context.Context,
	sessionID string,
	lineage *promptLineage,
	bundle *responseBundle,
) (*database.EntryEntity, error) {
	persistCtx, cancel := runtime.promptPersistenceContext(ctx, assistantBundleWriteCount(bundle))
	defer cancel()

	return runtime.persistAssistantBundle(persistCtx, sessionID, lineage, bundle)
}

func (runtime *Runtime) persistAssistantBundle(
	ctx context.Context,
	sessionID string,
	lineage *promptLineage,
	bundle *responseBundle,
) (*database.EntryEntity, error) {
	parentID, err := runtime.appendAssistantSideEffects(ctx, sessionID, &lineage.activeParentEntryID, bundle)
	if err != nil {
		if parentID != nil {
			lineage.activeParentEntryID = *parentID
		}

		return nil, err
	}

	if parentID != nil {
		lineage.activeParentEntryID = *parentID
	}

	entry, err := runtime.appendAssistantResponseEntry(ctx, sessionID, parentID, bundle)
	if err != nil {
		return nil, oops.In("assistant").Code("append_assistant").Wrapf(err, "append assistant message")
	}

	lineage.adopt(entry)

	return entry, nil
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
		request.Images,
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

const (
	agentTaskServiceUnavailable = "agent task service is unavailable"
	toolTaskServiceUnavailable  = "tool task service is unavailable"
)

// ToolTasks returns durable background tool tasks owned by a session.
func (runtime *Runtime) ToolTasks(
	ctx context.Context,
	ownerSessionID string,
	states []database.TaskState,
	limit int,
) ([]database.ToolTaskEntity, error) {
	if runtime == nil || runtime.toolTasks == nil {
		return nil, oops.In("assistant").Code("tool_task_service_unavailable").
			Errorf(toolTaskServiceUnavailable)
	}

	tasks, err := runtime.toolTasks.List(ctx, ownerSessionID, states, limit)
	if err != nil {
		return nil, oops.In("assistant").Code("list_tool_tasks").Wrapf(err, "list tool tasks")
	}

	return tasks, nil
}

// ToolTask returns one owner-scoped durable background tool task.
func (runtime *Runtime) ToolTask(
	ctx context.Context,
	ownerSessionID string,
	taskID string,
) (*database.ToolTaskEntity, bool, error) {
	if runtime == nil || runtime.toolTasks == nil {
		return nil, false, oops.In("assistant").Code("tool_task_service_unavailable").
			Errorf(toolTaskServiceUnavailable)
	}

	task, found, err := runtime.toolTasks.Get(ctx, ownerSessionID, taskID)
	if err != nil {
		return nil, false, oops.In("assistant").Code("get_tool_task").Wrapf(err, "get tool task")
	}

	return task, found, nil
}

// CancelToolTask requests owner-scoped cancellation of durable background tool work.
func (runtime *Runtime) CancelToolTask(
	ctx context.Context,
	ownerSessionID string,
	taskID string,
) (*database.ToolTaskEntity, bool, error) {
	if runtime == nil || runtime.toolTasks == nil {
		return nil, false, oops.In("assistant").Code("tool_task_service_unavailable").
			Errorf(toolTaskServiceUnavailable)
	}

	task, found, err := runtime.toolTasks.Cancel(ctx, ownerSessionID, taskID)
	if err != nil {
		return nil, false, oops.In("assistant").Code("cancel_tool_task").Wrapf(err, "cancel tool task")
	}

	return task, found, nil
}

type toolTaskCompletionSubscriber interface {
	SubscribeCompletions() (<-chan tooltask.Completion, func())
}

// SubscribeToolTaskCompletions follows locally completed durable tool executions.
func (runtime *Runtime) SubscribeToolTaskCompletions() (
	events <-chan tooltask.Completion, cancel func(), err error,
) {
	if runtime == nil || runtime.toolTasks == nil {
		return nil, nil, oops.In("assistant").Code("tool_task_service_unavailable").
			Errorf(toolTaskServiceUnavailable)
	}

	subscriber, ok := runtime.toolTasks.(toolTaskCompletionSubscriber)
	if !ok {
		return nil, nil, oops.In("assistant").Code("tool_task_completions_unavailable").
			Errorf("tool task completions are unavailable")
	}

	events, cancel = subscriber.SubscribeCompletions()

	return events, cancel, nil
}

// AgentTasks returns durable agent tasks owned by a session.
func (runtime *Runtime) AgentTasks(
	ctx context.Context,
	ownerSessionID string,
	limit int,
) ([]database.AgentTaskEntity, error) {
	if runtime == nil || runtime.agentTasks == nil {
		return nil, oops.In("assistant").Code("agent_task_service_unavailable").
			Errorf(agentTaskServiceUnavailable)
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
		return nil, false, oops.In("assistant").Code("agent_task_service_unavailable").
			Errorf(agentTaskServiceUnavailable)
	}

	task, found, err := runtime.agentTasks.Get(ctx, taskID)
	if err != nil {
		return nil, false, oops.In("assistant").Code("get_agent_task").Wrapf(err, "get agent task")
	}

	return task, found, nil
}

type agentTaskEventReader interface {
	Events(context.Context, string, int64, int) ([]database.TaskEventEntity, error)
}

// AgentTaskEvents returns durable stream events for one agent task after a sequence.
func (runtime *Runtime) AgentTaskEvents(
	ctx context.Context,
	taskID string,
	after int64,
	limit int,
) ([]database.TaskEventEntity, error) {
	if runtime == nil || runtime.agentTasks == nil {
		return nil, oops.In("assistant").Code("agent_task_service_unavailable").
			Errorf(agentTaskServiceUnavailable)
	}

	reader, ok := runtime.agentTasks.(agentTaskEventReader)
	if !ok {
		return nil, oops.In("assistant").Code("agent_task_events_unavailable").
			Errorf("agent task events are unavailable")
	}

	events, err := reader.Events(ctx, taskID, after, limit)
	if err != nil {
		return nil, oops.In("assistant").Code("list_agent_task_events").
			Wrapf(err, "list agent task events")
	}

	return events, nil
}

// SubscribeAgentTask follows persisted events for one agent task.
func (runtime *Runtime) SubscribeAgentTask(
	taskID string,
) (events <-chan database.TaskEventEntity, cancel func(), err error) {
	if runtime == nil || runtime.agentTasks == nil {
		return nil, nil, oops.In("assistant").Code("agent_task_service_unavailable").
			Errorf(agentTaskServiceUnavailable)
	}

	events, cancel, err = runtime.agentTasks.SubscribeAgentTask(taskID)
	if err != nil {
		return nil, nil, oops.In("assistant").Code("subscribe_agent_task").Wrapf(err, "subscribe agent task")
	}

	return events, cancel, nil
}

// CancelAgentTask requests cancellation of one durable agent task.
func (runtime *Runtime) CancelAgentTask(
	ctx context.Context,
	ownerSessionID string,
	taskID string,
) (*database.TaskEntity, bool, error) {
	if runtime == nil || runtime.agentTasks == nil {
		return nil, false, oops.In("assistant").Code("agent_task_service_unavailable").
			Errorf(agentTaskServiceUnavailable)
	}

	task, found, err := runtime.agentTasks.Cancel(ctx, ownerSessionID, taskID, database.CancelSourceParent)
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
		agentTasks: runtime.agentTasks, workflowSubmitter: runtime.workflowSubmitter,
		toolTasks: runtime.toolTasks, toolCoordinator: runtime.toolCoordinator,
		attachments: runtime.attachments,
		operations:  runtime.operations, steering: runtime.steering,
		newCompactionUUID: runtime.newCompactionUUID, profile: clonedProfile,
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
	trimmedPrompt := strings.TrimPrefix(strings.TrimSpace(prompt), slashPrefix)

	trimmedPrompt = strings.TrimSpace(trimmedPrompt)
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
