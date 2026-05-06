// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
)

const slashPrefix = "/"

// Runtime coordinates local prompt handling and durable sessions.
type Runtime struct {
	cfg        *config.Config
	store      *database.SessionStore
	extensions *extension.Manager
	cache      *ResponseCache
	logger     *slog.Logger
}

// PromptRequest contains one user prompt invocation.
type PromptRequest struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	Text      string `json:"text"`
	Name      string `json:"name"`
}

// PromptResponse describes persisted prompt output.
type PromptResponse struct {
	SessionID        string `json:"session_id"`
	UserEntryID      string `json:"user_entry_id"`
	AssistantEntryID string `json:"assistant_entry_id"`
	Text             string `json:"text"`
	Cached           bool   `json:"cached"`
}

// NewRuntime creates an assistant runtime.
func NewRuntime(
	cfg *config.Config,
	store *database.SessionStore,
	extensions *extension.Manager,
	cache *ResponseCache,
	logger *slog.Logger,
) *Runtime {
	return &Runtime{
		cfg:        cfg,
		store:      store,
		extensions: extensions,
		cache:      cache,
		logger:     logger,
	}
}

// Prompt appends a user prompt and a local assistant response to the selected session.
func (runtime *Runtime) Prompt(ctx context.Context, request PromptRequest) (*PromptResponse, error) {
	activeSession, err := runtime.resolveSession(ctx, request)
	if err != nil {
		return nil, err
	}

	leaf, _, err := runtime.store.LeafEntry(ctx, activeSession.ID)
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
	userEntry, err := runtime.store.AppendMessage(ctx, activeSession.ID, parentIDFromEntry(leaf), &userMessage)
	if err != nil {
		return nil, oops.In("assistant").Code("append_user").Wrapf(err, "append user message")
	}

	emitErr := runtime.extensions.Emit(ctx, "before_agent_start", map[string]any{"prompt": request.Text})
	if emitErr != nil {
		return nil, oops.In("assistant").Code("before_agent_start").Wrapf(emitErr, "emit before_agent_start")
	}

	responseText, cached, err := runtime.respond(ctx, activeSession.ID, request.CWD, request.Text)
	if err != nil {
		return nil, err
	}

	assistantMessage := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleAssistant,
		Content:   responseText,
		Provider:  runtime.cfg.Assistant.Provider,
		Model:     runtime.cfg.Assistant.Model,
	}
	assistantEntry, err := runtime.store.AppendMessage(ctx, activeSession.ID, &userEntry.ID, &assistantMessage)
	if err != nil {
		return nil, oops.In("assistant").Code("append_assistant").Wrapf(err, "append assistant message")
	}

	emitErr = runtime.extensions.Emit(ctx, "agent_end", map[string]any{"response": responseText})
	if emitErr != nil {
		return nil, oops.In("assistant").Code("assistant_end").Wrapf(emitErr, "emit assistant end")
	}

	return &PromptResponse{
		SessionID:        activeSession.ID,
		UserEntryID:      userEntry.ID,
		AssistantEntryID: assistantEntry.ID,
		Text:             responseText,
		Cached:           cached,
	}, nil
}

// SessionStore returns the underlying session store for command and UI layers.
func (runtime *Runtime) SessionStore() *database.SessionStore {
	return runtime.store
}

func (runtime *Runtime) resolveSession(ctx context.Context, request PromptRequest) (*database.SessionEntity, error) {
	if request.SessionID != "" {
		loadedSession, found, err := runtime.store.GetSession(ctx, request.SessionID)
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

	if request.Name != "" {
		return runtime.store.CreateSession(ctx, request.CWD, request.Name, "")
	}

	latestSession, found, err := runtime.store.LatestSession(ctx, request.CWD)
	if err != nil {
		return nil, err
	}
	if found {
		return latestSession, nil
	}

	return runtime.store.CreateSession(ctx, request.CWD, "", "")
}

func (runtime *Runtime) respond(ctx context.Context, sessionID, cwd, prompt string) (
	response string,
	cached bool,
	err error,
) {
	if strings.HasPrefix(prompt, slashPrefix) {
		slashResponse, slashErr := runtime.respondToSlashCommand(ctx, cwd, prompt)
		return slashResponse, false, slashErr
	}

	cacheKey := runtime.cacheKey(sessionID, prompt)
	cachedResponse, found, err := runtime.cache.Get(cacheKey)
	if err != nil {
		return "", false, oops.In("assistant").Code("cache_get").Wrapf(err, "read response cache")
	}
	if found {
		return cachedResponse, true, nil
	}

	response = runtime.localResponse(prompt)
	runtime.cache.Set(cacheKey, response)

	return response, false, nil
}

func (runtime *Runtime) respondToSlashCommand(ctx context.Context, cwd, prompt string) (string, error) {
	commandName, commandArgs := splitSlashCommand(prompt)
	if commandName == "" {
		return "", fmt.Errorf("assistant: empty slash command")
	}

	if commandName == "tool" {
		return runtime.respondToToolCommand(ctx, cwd, commandArgs)
	}

	response, err := runtime.extensions.ExecuteCommand(ctx, commandName, commandArgs)
	if err != nil {
		return "", oops.
			In("assistant").
			Code("extension_command").
			With("command", commandName).
			Wrapf(err, "execute command")
	}

	return response, nil
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

func (runtime *Runtime) localResponse(prompt string) string {
	commands := runtime.extensions.Commands()
	extensionTools := runtime.extensions.Tools()
	builtInTools := tool.AllDefinitions()
	modelLine := fmt.Sprintf(
		"provider=%s model=%s thinking=%s",
		runtime.cfg.Assistant.Provider,
		runtime.cfg.Assistant.Model,
		runtime.cfg.Assistant.ThinkingLevel,
	)
	parts := []string{
		"librecode-go local runtime is wired and ready.",
		modelLine,
		fmt.Sprintf("prompt=%q", prompt),
		fmt.Sprintf(
			"extension_commands=%d extension_tools=%d built_in_tools=%d",
			len(commands),
			len(extensionTools),
			len(builtInTools),
		),
	}

	if len(commands) > 0 {
		parts = append(parts, "commands="+joinCommandNames(commands))
	}
	if len(extensionTools) > 0 {
		parts = append(parts, "extension_tools="+joinExtensionToolNames(extensionTools))
	}
	if len(builtInTools) > 0 {
		parts = append(parts, "built_in_tools="+joinBuiltInToolNames(builtInTools))
	}

	return strings.Join(parts, "\n")
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

	commandName, commandArgs, found := strings.Cut(trimmedPrompt, " ")
	if !found {
		return commandName, ""
	}

	return commandName, strings.TrimSpace(commandArgs)
}

func joinCommandNames(commands []extension.Command) string {
	names := lo.Map(commands, func(command extension.Command, _ int) string {
		return command.Name
	})

	return strings.Join(names, ",")
}

func joinExtensionToolNames(tools []extension.Tool) string {
	names := lo.Map(tools, func(tool extension.Tool, _ int) string {
		return tool.Name
	})

	return strings.Join(names, ",")
}

func joinBuiltInToolNames(definitions []tool.Definition) string {
	names := lo.Map(definitions, func(definition tool.Definition, _ int) string {
		return string(definition.Name)
	})

	return strings.Join(names, ",")
}

// DefaultCWD returns an absolute working directory for prompt requests.
func DefaultCWD(cwd string) (string, error) {
	if cwd == "" {
		return filepath.Abs(".")
	}

	return filepath.Abs(cwd)
}
