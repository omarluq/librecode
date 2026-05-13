// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

const slashPrefix = "/"

// Runtime coordinates prompt handling and durable sessions.
type Runtime struct {
	cfg        *config.Config
	sessions   *database.SessionRepository
	extensions *extension.Manager
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
	Usage            model.TokenUsage `json:"usage,omitempty"`
	Cached           bool             `json:"cached"`
}

type responseBundle struct {
	Text       string
	Thinking   []string
	ToolEvents []ToolEvent
	Usage      model.TokenUsage
}

// NewRuntime creates an assistant runtime.
func NewRuntime(
	cfg *config.Config,
	sessions *database.SessionRepository,
	extensions *extension.Manager,
	cache *ResponseCache,
	events *event.Bus,
	models *model.Registry,
	client CompletionClient,
	logger *slog.Logger,
) *Runtime {
	if client == nil {
		client = NewHTTPCompletionClient()
	}
	return &Runtime{
		cfg:        cfg,
		sessions:   sessions,
		extensions: extensions,
		cache:      cache,
		events:     events,
		models:     models,
		client:     client,
		logger:     logger,
	}
}

// Prompt appends a user prompt and an assistant response to the selected session.
func (runtime *Runtime) Prompt(ctx context.Context, request *PromptRequest) (*PromptResponse, error) {
	if request == nil {
		return nil, oops.In("assistant").Code("nil_prompt_request").Errorf("prompt request is nil")
	}
	activeSession, err := runtime.resolveSession(ctx, request)
	if err != nil {
		return nil, err
	}

	parentID, err := runtime.promptParentID(ctx, activeSession.ID, request.ParentEntryID)
	if err != nil {
		return nil, err
	}

	userMessage := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   request.Text,
		Provider:  "",
		Model:     "",
	}
	userEntry, err := runtime.sessions.AppendMessage(ctx, activeSession.ID, parentID, &userMessage)
	if err != nil {
		return nil, oops.In("assistant").Code("append_user").Wrapf(err, "append user message")
	}
	runtime.notifyPromptUserEntry(request, activeSession.ID, userEntry.ID)

	runtime.emit(ctx, "before_agent_start", map[string]any{"prompt": request.Text})
	emitErr := runtime.extensions.Emit(ctx, "before_agent_start", map[string]any{"prompt": request.Text})
	if emitErr != nil {
		return nil, oops.In("assistant").Code("before_agent_start").Wrapf(emitErr, "emit before_agent_start")
	}

	bundle, cached, err := runtime.respond(
		ctx,
		activeSession.ID,
		request.CWD,
		request.Text,
		request.OnEvent,
		request.OnRetry,
	)
	if err != nil {
		return nil, err
	}

	assistantParentID, err := runtime.appendAssistantSideEffects(ctx, activeSession.ID, userEntry.ID, bundle)
	if err != nil {
		return nil, err
	}
	assistantMessage := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleAssistant,
		Content:   bundle.Text,
		Provider:  runtime.cfg.Assistant.Provider,
		Model:     runtime.cfg.Assistant.Model,
	}
	assistantEntry, err := runtime.sessions.AppendMessage(ctx, activeSession.ID, assistantParentID, &assistantMessage)
	if err != nil {
		return nil, oops.In("assistant").Code("append_assistant").Wrapf(err, "append assistant message")
	}

	runtime.emit(ctx, "agent_end", map[string]any{"response": bundle.Text})
	emitErr = runtime.extensions.Emit(ctx, "agent_end", map[string]any{"response": bundle.Text})
	if emitErr != nil {
		return nil, oops.In("assistant").Code("assistant_end").Wrapf(emitErr, "emit assistant end")
	}

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

// SessionRepository returns the underlying session repository for command and UI layers.
func (runtime *Runtime) SessionRepository() *database.SessionRepository {
	return runtime.sessions
}

// ModelRegistry returns the model registry used by the runtime.
func (runtime *Runtime) ModelRegistry() *model.Registry {
	return runtime.models
}

func (runtime *Runtime) emit(ctx context.Context, channel string, data any) {
	if runtime.events == nil {
		return
	}

	runtime.events.Emit(ctx, channel, data)
}

func (runtime *Runtime) appendAssistantSideEffects(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	bundle *responseBundle,
) (*string, error) {
	parentID := &userEntryID
	for _, thinking := range bundle.Thinking {
		trimmed := strings.TrimSpace(thinking)
		if trimmed == "" {
			continue
		}
		message := database.MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      database.RoleThinking,
			Content:   trimmed,
			Provider:  runtime.cfg.Assistant.Provider,
			Model:     runtime.cfg.Assistant.Model,
		}
		entry, err := runtime.sessions.AppendMessage(ctx, sessionID, parentID, &message)
		if err != nil {
			return nil, oops.In("assistant").Code("append_thinking").Wrapf(err, "append thinking message")
		}
		parentID = &entry.ID
	}
	for _, event := range bundle.ToolEvents {
		message := database.MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      database.RoleToolResult,
			Content:   formatToolEvent(&event),
			Provider:  runtime.cfg.Assistant.Provider,
			Model:     runtime.cfg.Assistant.Model,
		}
		entry, err := runtime.sessions.AppendMessage(ctx, sessionID, parentID, &message)
		if err != nil {
			return nil, oops.In("assistant").Code("append_tool_result").Wrapf(err, "append tool result")
		}
		parentID = &entry.ID
	}

	return parentID, nil
}

func formatToolEvent(toolEvent *ToolEvent) string {
	parts := []string{fmt.Sprintf("tool: %s", toolEvent.Name)}
	if strings.TrimSpace(toolEvent.ArgumentsJSON) != "" {
		parts = append(parts, "arguments:", toolEvent.ArgumentsJSON)
	}
	if toolEvent.Error != "" {
		parts = append(parts, "error:", toolEvent.Error)
	}
	if strings.TrimSpace(toolEvent.DetailsJSON) != "" {
		parts = append(parts, "details:", toolEvent.DetailsJSON)
	}
	if strings.TrimSpace(toolEvent.Result) != "" {
		parts = append(parts, "output:", toolEvent.Result)
	}

	return strings.Join(parts, "\n")
}

func (runtime *Runtime) resolveSession(ctx context.Context, request *PromptRequest) (*database.SessionEntity, error) {
	if request.SessionID != "" {
		if request.ResumeLatest {
			return nil, oops.
				In("assistant").
				Code("session_selection_conflict").
				Errorf("resume latest cannot be used with an explicit session")
		}
		loadedSession, found, err := runtime.sessions.GetSession(ctx, request.SessionID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, oops.
				In("assistant").
				Code("session_not_found").
				With("session_id", request.SessionID).
				Errorf("session not found")
		}

		return loadedSession, nil
	}

	if request.ResumeLatest {
		if request.Name != "" {
			return nil, oops.
				In("assistant").
				Code("session_selection_conflict").
				Errorf("resume latest cannot be used with a new session name")
		}
		latestSession, found, err := runtime.sessions.LatestSession(ctx, request.CWD)
		if err != nil {
			return nil, err
		}
		if found {
			return latestSession, nil
		}
	}

	if request.Name != "" {
		return runtime.sessions.CreateSession(ctx, request.CWD, request.Name, "")
	}

	return runtime.sessions.CreateSession(ctx, request.CWD, "", "")
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

func (runtime *Runtime) respond(
	ctx context.Context,
	sessionID string,
	cwd string,
	prompt string,
	onEvent func(StreamEvent),
	onRetry RetryEventHandler,
) (
	bundle *responseBundle,
	cached bool,
	err error,
) {
	if strings.HasPrefix(prompt, slashPrefix) {
		slashResponse, slashToolEvents, slashErr := runtime.respondToSlashCommand(ctx, cwd, prompt, onEvent)
		return &responseBundle{
			Text:       slashResponse,
			Thinking:   nil,
			ToolEvents: slashToolEvents,
			Usage:      model.EmptyTokenUsage(),
		}, false, slashErr
	}

	cacheKey := runtime.cacheKey(sessionID, prompt)
	cachedResponse, found, err := runtime.cache.Get(cacheKey)
	if err != nil {
		return nil, false, oops.In("assistant").Code("cache_get").Wrapf(err, "read response cache")
	}
	if found {
		return &responseBundle{
			Text:       cachedResponse,
			Thinking:   nil,
			ToolEvents: nil,
			Usage:      model.EmptyTokenUsage(),
		}, true, nil
	}

	bundle, err = runtime.modelResponse(ctx, sessionID, cwd, prompt, onEvent, onRetry)
	if err != nil {
		return nil, false, err
	}
	runtime.cache.Set(cacheKey, bundle.Text)

	return bundle, false, nil
}

func (runtime *Runtime) respondToSlashCommand(
	ctx context.Context,
	cwd string,
	prompt string,
	onEvent func(StreamEvent),
) (string, []ToolEvent, error) {
	commandName, commandArgs := splitSlashCommand(prompt)
	if commandName == "" {
		return "", nil, fmt.Errorf("assistant: empty slash command")
	}

	if commandName == "skill" {
		return runtime.respondToSkillCommand(ctx, cwd, commandArgs, onEvent)
	}
	if commandName == "tool" {
		response, err := runtime.respondToToolCommand(ctx, cwd, commandArgs)
		return response, nil, err
	}

	response, err := runtime.extensions.ExecuteCommand(ctx, commandName, commandArgs)
	if err != nil {
		return "", nil, oops.
			In("assistant").
			Code("extension_command").
			With("command", commandName).
			Wrapf(err, "execute command")
	}

	return response, nil, nil
}

func (runtime *Runtime) respondToSkillCommand(
	ctx context.Context,
	cwd string,
	args string,
	onEvent func(StreamEvent),
) (string, []ToolEvent, error) {
	skills := core.LoadSkills(cwd, nil, true).Skills
	name := strings.TrimSpace(args)
	if name == "" {
		if len(skills) == 0 {
			return "No skills found.", nil, nil
		}
		lines := []string{"Available skills:"}
		for index := range skills {
			lines = append(lines, fmt.Sprintf("- %s: %s", skills[index].Name, skills[index].Description))
		}

		return strings.Join(lines, "\n"), nil, nil
	}

	for index := range skills {
		skill := &skills[index]
		if skill.Name != name {
			continue
		}
		result, toolEvent, err := runtime.loadSkillWithReadTool(ctx, cwd, skill, nil)
		if err != nil {
			return "", nil, err
		}
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: &toolEvent,
			Usage:     nil,
			Kind:      StreamEventSkillLoaded,
			Text:      skill.Name,
		})

		return result, []ToolEvent{toolEvent}, nil
	}

	return "", nil, fmt.Errorf("assistant: skill %q not found", name)
}

func (runtime *Runtime) loadSkillWithReadTool(
	ctx context.Context,
	cwd string,
	skill *core.Skill,
	limit *int,
) (string, ToolEvent, error) {
	registry := tool.NewRegistry(cwd)
	input := map[string]any{jsonPathKey: skill.FilePath}
	if limit != nil {
		input["limit"] = *limit
	}
	result, err := registry.Execute(ctx, string(tool.NameRead), input)
	toolEvent := ToolEvent{
		Name:          "load skill: " + skill.Name,
		ArgumentsJSON: skillReadArgumentsJSON(skill.FilePath, limit),
		DetailsJSON:   "",
		Result:        result.Text(),
		Error:         "",
	}
	if err != nil {
		toolEvent.Error = err.Error()
		return "", toolEvent, oops.In("assistant").Code("skill_read").Wrapf(err, "load skill with read tool")
	}

	return result.Text(), toolEvent, nil
}

func skillReadArgumentsJSON(path string, limit *int) string {
	if limit == nil {
		return fmt.Sprintf(`{"path":%q}`, path)
	}

	return fmt.Sprintf(`{"path":%q,"limit":%d}`, path, *limit)
}

func (runtime *Runtime) respondToToolCommand(ctx context.Context, cwd, args string) (string, error) {
	toolName, payload, found := strings.Cut(strings.TrimSpace(args), " ")
	if toolName == "" {
		return "", fmt.Errorf("assistant: tool command requires a tool name")
	}
	if !found || strings.TrimSpace(payload) == "" {
		payload = "{}"
	}

	registry := tool.NewRegistry(cwd)
	result, err := registry.ExecuteJSON(ctx, toolName, []byte(payload))
	if err != nil {
		return "", oops.
			In("assistant").
			Code("builtin_tool").
			With("tool", toolName).
			Wrapf(err, "execute built-in tool")
	}

	return result.Text(), nil
}

func (runtime *Runtime) modelResponse(
	ctx context.Context,
	sessionID string,
	cwd string,
	prompt string,
	onEvent func(StreamEvent),
	onRetry RetryEventHandler,
) (*responseBundle, error) {
	if runtime.models == nil {
		return nil, oops.In("assistant").Code("models_unavailable").Errorf("model registry is not configured")
	}
	selectedModel, err := runtime.selectedModel()
	if err != nil {
		return nil, err
	}
	auth := runtime.models.RequestAuthContext(ctx, selectedModel.Provider)
	if !auth.OK {
		return nil, oops.In("assistant").
			Code("auth_missing").
			With("provider", selectedModel.Provider).
			Wrapf(fmt.Errorf("%s", auth.Error), "resolve model auth")
	}
	messages, err := runtime.modelContextMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	systemPrompt := defaultSystemPrompt(cwd)
	skillActivation := core.AutoActivateSkillsDetailed(prompt, core.LoadSkills(cwd, nil, true).Skills)
	activeSkills := skillActivation.Activated
	if len(skillActivation.Diagnostics) > 0 {
		runtime.logger.Debug("skill auto-activation diagnostics", slog.Int("count", len(skillActivation.Diagnostics)))
	}
	for index := range skillActivation.Matches {
		match := &skillActivation.Matches[index]
		runtime.logger.Debug(
			"skill auto-activated",
			slog.String("skill", match.Skill.Name),
			slog.String("reason", match.Reason),
			slog.Int("score", match.Score),
		)
	}
	runtime.emitActivatedSkillReads(ctx, cwd, activeSkills, onEvent)
	if len(activeSkills) > 0 {
		systemPrompt += core.FormatActiveSkillsForPrompt(activeSkills)
		payload := map[string]any{
			"skills":  activeSkillEventPayload(activeSkills),
			"matches": activeSkillMatchPayload(skillActivation.Matches),
		}
		runtime.emit(ctx, "skill_auto_activate", payload)
		if emitErr := runtime.extensions.Emit(ctx, "skill_auto_activate", payload); emitErr != nil {
			return nil, oops.In("assistant").Code("skill_auto_activate").Wrapf(emitErr, "emit skill auto activation")
		}
	}

	estimatedUsage := estimateTokenUsage(systemPrompt, messages, &selectedModel)
	runtime.emitUsage(ctx, onEvent, estimatedUsage)
	request := &CompletionRequest{
		Model:         selectedModel,
		Auth:          auth,
		Messages:      messages,
		SessionID:     sessionID,
		SystemPrompt:  systemPrompt,
		ThinkingLevel: runtime.cfg.Assistant.ThinkingLevel,
		CWD:           cwd,
		OnEvent:       onEvent,
	}
	result, err := runtime.completeWithRetry(ctx, request, onRetry)
	if err != nil {
		return nil, err
	}
	usage := mergeUsage(estimatedUsage, result.Usage)
	runtime.emitUsage(ctx, onEvent, usage)

	return &responseBundle{
		Text:       result.Text,
		Thinking:   result.Thinking,
		ToolEvents: result.ToolEvents,
		Usage:      usage,
	}, nil
}

func (runtime *Runtime) completeWithRetry(
	ctx context.Context,
	request *CompletionRequest,
	onRetry RetryEventHandler,
) (*CompletionResult, error) {
	retry := retryConfig(runtime.cfg)
	if !retry.Enabled || retry.MaxAttempts <= 1 {
		return runtime.client.Complete(ctx, request)
	}

	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		result, err := runtime.client.Complete(ctx, request)
		if err == nil {
			if attempt > 1 {
				runtime.emitRetryEvent(ctx, onRetry, RetryEvent{
					Kind:        RetryEventEnd,
					Error:       "",
					Attempt:     attempt,
					MaxAttempts: retry.MaxAttempts,
					Delay:       0,
				})
			}
			return result, nil
		}
		lastErr = err
		if attempt == retry.MaxAttempts || !ShouldRetryModelError(err) {
			return nil, err
		}
		delay := retryDelay(attempt, retry)
		runtime.emitRetryEvent(ctx, onRetry, RetryEvent{
			Kind:        RetryEventStart,
			Attempt:     attempt + 1,
			MaxAttempts: retry.MaxAttempts,
			Delay:       delay,
			Error:       err.Error(),
		})
		if waitErr := waitForRetry(ctx, delay); waitErr != nil {
			return nil, oops.In("assistant").Code("retry_canceled").Wrapf(waitErr, "wait before retry")
		}
	}

	return nil, lastErr
}

func (runtime *Runtime) emitRetryEvent(ctx context.Context, handler RetryEventHandler, retryEvent RetryEvent) {
	if handler != nil {
		handler(retryEvent)
	}
	runtime.emit(ctx, string(retryEvent.Kind), retryEvent)
	if runtime.extensions == nil {
		return
	}
	if err := runtime.extensions.Emit(ctx, string(retryEvent.Kind), map[string]any{
		"attempt":      retryEvent.Attempt,
		"max_attempts": retryEvent.MaxAttempts,
		"delay_ms":     retryEvent.Delay.Milliseconds(),
		"error":        retryEvent.Error,
	}); err != nil && runtime.logger != nil {
		runtime.logger.Debug("extension retry event failed", "event", retryEvent.Kind, "error", err)
	}
}

func (runtime *Runtime) selectedModel() (model.Model, error) {
	provider := runtime.cfg.Assistant.Provider
	modelID := runtime.cfg.Assistant.Model
	models := runtime.models.All()
	for index := range models {
		candidate := &models[index]
		if candidate.Provider == provider && candidate.ID == modelID {
			return *candidate, nil
		}
	}
	if provider == "" || modelID == "" {
		return model.Model{}, oops.In("assistant").Code("model_missing").Errorf("select a model with /model or /login")
	}

	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         provider,
		ID:               modelID,
		Name:             modelID,
		API:              "openai-completions",
		BaseURL:          "",
		Input:            []model.InputMode{model.InputText},
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}, nil
}

func (runtime *Runtime) emitActivatedSkillReads(
	ctx context.Context,
	cwd string,
	skills []core.ActivatedSkill,
	onEvent func(StreamEvent),
) []ToolEvent {
	if len(skills) == 0 {
		return nil
	}
	limit := maxActiveSkillReadLines()
	toolEvents := make([]ToolEvent, 0, len(skills))
	for index := range skills {
		skill := &skills[index].Skill
		_, toolEvent, err := runtime.loadSkillWithReadTool(ctx, cwd, skill, &limit)
		if err != nil {
			runtime.logger.Debug(
				"failed to emit activated skill read",
				slog.String("skill", skill.Name),
				slog.Any("error", err),
			)
		}
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: &toolEvent,
			Usage:     nil,
			Kind:      StreamEventSkillLoaded,
			Text:      skill.Name,
		})
		toolEvents = append(toolEvents, toolEvent)
	}

	return toolEvents
}

func maxActiveSkillReadLines() int {
	return 2000
}

func activeSkillEventPayload(skills []core.ActivatedSkill) []map[string]any {
	payload := make([]map[string]any, 0, len(skills))
	for index := range skills {
		skill := skills[index].Skill
		payload = append(payload, map[string]any{
			"name":        skill.Name,
			"description": skill.Description,
			"path":        skill.FilePath,
			"truncated":   skills[index].Truncated,
		})
	}

	return payload
}

func activeSkillMatchPayload(matches []core.SkillActivationDiagnostic) []map[string]any {
	payload := make([]map[string]any, 0, len(matches))
	for index := range matches {
		match := matches[index]
		payload = append(payload, map[string]any{
			"name":   match.Skill.Name,
			"path":   match.Skill.FilePath,
			"reason": match.Reason,
			"score":  match.Score,
		})
	}

	return payload
}

func (runtime *Runtime) modelContextMessages(ctx context.Context, sessionID string) ([]database.MessageEntity, error) {
	leafEntry, _, err := runtime.sessions.LeafEntry(ctx, sessionID)
	if err != nil {
		return nil, oops.In("assistant").Code("load_context_leaf").Wrapf(err, "load session leaf")
	}
	leafID := ""
	if leafEntry != nil {
		leafID = leafEntry.ID
	}
	contextEntity, err := runtime.sessions.BuildContext(ctx, sessionID, leafID)
	if err != nil {
		return nil, oops.In("assistant").Code("load_context").Wrapf(err, "load session context")
	}

	return modelFacingMessages(contextEntity.Messages), nil
}

func modelFacingMessages(messages []database.MessageEntity) []database.MessageEntity {
	filtered := make([]database.MessageEntity, 0, len(messages))
	for index := range messages {
		message := messages[index]
		if !isModelFacingRole(message.Role) || strings.TrimSpace(message.Content) == "" {
			continue
		}
		filtered = append(filtered, message)
	}

	return filtered
}

func isModelFacingRole(role database.Role) bool {
	switch role {
	case database.RoleUser, database.RoleAssistant:
		return true
	case database.RoleToolResult,
		database.RoleThinking,
		database.RoleCustom,
		database.RoleBashExecution,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return false
	}

	return false
}

func defaultSystemPrompt(cwd string) string {
	prompt := strings.Join([]string{
		"You are librecode, an AI coding assistant. Be concise, helpful, and accurate.",
		"You are running inside a local filesystem workspace.",
		fmt.Sprintf("Current working directory: %s", cwd),
		"Use built-in tools (ls, find, grep, read, bash, edit, write) " +
			"to inspect or change workspace files when needed.",
		"Do not claim you cannot access files; inspect them with tools instead.",
		"Respect .gitignore and default ignored paths; avoid ignored files unless explicitly needed.",
		"Use the fewest tool calls needed; once you have enough evidence, stop using tools and answer.",
	}, "\n")
	skills := core.LoadSkills(cwd, nil, true).Skills
	if len(skills) > 0 {
		prompt += core.FormatSkillsForPrompt(skills)
	}

	return prompt
}

func (runtime *Runtime) cacheKey(sessionID, prompt string) string {
	return strings.Join(
		[]string{runtime.cfg.Assistant.Provider, runtime.cfg.Assistant.Model, sessionID, prompt},
		"\x00",
	)
}

func parentIDFromEntry(entry *database.EntryEntity) *string {
	if entry == nil {
		return nil
	}

	return &entry.ID
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
