// Package agent orchestrates sessions, plugins, cache, and prompt execution.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/plugin"
	"github.com/omarluq/librecode/internal/session"
)

const slashPrefix = "/"

// Runtime coordinates local prompt handling and durable sessions.
type Runtime struct {
	cfg     *config.Config
	store   *session.Store
	plugins *plugin.Manager
	cache   *ResponseCache
	logger  *slog.Logger
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

// NewRuntime creates an agent runtime.
func NewRuntime(
	cfg *config.Config,
	store *session.Store,
	plugins *plugin.Manager,
	cache *ResponseCache,
	logger *slog.Logger,
) *Runtime {
	return &Runtime{
		cfg:     cfg,
		store:   store,
		plugins: plugins,
		cache:   cache,
		logger:  logger,
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

	userEntry, err := runtime.store.AppendMessage(ctx, activeSession.ID, parentIDFromEntry(leaf), session.Message{
		Role:      session.RoleUser,
		Content:   request.Text,
		Provider:  "",
		Model:     "",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		return nil, oops.In("agent").Code("append_user").Wrapf(err, "append user message")
	}

	if err := runtime.plugins.Emit(ctx, "before_agent_start", map[string]any{"prompt": request.Text}); err != nil {
		return nil, oops.In("agent").Code("before_agent_start").Wrapf(err, "emit before_agent_start")
	}

	responseText, cached, err := runtime.respond(ctx, activeSession.ID, request.Text)
	if err != nil {
		return nil, err
	}

	assistantEntry, err := runtime.store.AppendMessage(ctx, activeSession.ID, &userEntry.ID, session.Message{
		Role:      session.RoleAssistant,
		Content:   responseText,
		Provider:  runtime.cfg.Agent.Provider,
		Model:     runtime.cfg.Agent.Model,
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		return nil, oops.In("agent").Code("append_assistant").Wrapf(err, "append assistant message")
	}

	if err := runtime.plugins.Emit(ctx, "agent_end", map[string]any{"response": responseText}); err != nil {
		return nil, oops.In("agent").Code("agent_end").Wrapf(err, "emit agent_end")
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
func (runtime *Runtime) SessionStore() *session.Store {
	return runtime.store
}

func (runtime *Runtime) resolveSession(ctx context.Context, request PromptRequest) (*session.Session, error) {
	if request.SessionID != "" {
		loadedSession, found, err := runtime.store.GetSession(ctx, request.SessionID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, oops.In("agent").Code("session_not_found").With("session_id", request.SessionID).Errorf("session not found")
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

func (runtime *Runtime) respond(ctx context.Context, sessionID string, prompt string) (string, bool, error) {
	if strings.HasPrefix(prompt, slashPrefix) {
		response, err := runtime.respondToSlashCommand(ctx, prompt)
		return response, false, err
	}

	cacheKey := runtime.cacheKey(sessionID, prompt)
	cachedResponse, found, err := runtime.cache.Get(cacheKey)
	if err != nil {
		return "", false, oops.In("agent").Code("cache_get").Wrapf(err, "read response cache")
	}
	if found {
		return cachedResponse, true, nil
	}

	response := runtime.localResponse(prompt)
	runtime.cache.Set(cacheKey, response)

	return response, false, nil
}

func (runtime *Runtime) respondToSlashCommand(ctx context.Context, prompt string) (string, error) {
	commandName, commandArgs := splitSlashCommand(prompt)
	if commandName == "" {
		return "", fmt.Errorf("agent: empty slash command")
	}

	response, err := runtime.plugins.ExecuteCommand(ctx, commandName, commandArgs)
	if err != nil {
		return "", oops.In("agent").Code("plugin_command").With("command", commandName).Wrapf(err, "execute command")
	}

	return response, nil
}

func (runtime *Runtime) localResponse(prompt string) string {
	commands := runtime.plugins.Commands()
	tools := runtime.plugins.Tools()
	parts := []string{
		"librecode-go local runtime is wired and ready.",
		fmt.Sprintf("provider=%s model=%s thinking=%s", runtime.cfg.Agent.Provider, runtime.cfg.Agent.Model, runtime.cfg.Agent.ThinkingLevel),
		fmt.Sprintf("prompt=%q", prompt),
		fmt.Sprintf("lua_commands=%d lua_tools=%d", len(commands), len(tools)),
	}

	if len(commands) > 0 {
		parts = append(parts, "commands="+joinCommandNames(commands))
	}
	if len(tools) > 0 {
		parts = append(parts, "tools="+joinToolNames(tools))
	}

	return strings.Join(parts, "\n")
}

func (runtime *Runtime) cacheKey(sessionID string, prompt string) string {
	return strings.Join([]string{runtime.cfg.Agent.Provider, runtime.cfg.Agent.Model, sessionID, prompt}, "\x00")
}

func parentIDFromEntry(entry *session.Entry) *string {
	if entry == nil {
		return nil
	}

	return &entry.ID
}

func splitSlashCommand(prompt string) (string, string) {
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

func joinCommandNames(commands []plugin.Command) string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Name)
	}

	return strings.Join(names, ",")
}

func joinToolNames(tools []plugin.Tool) string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}

	return strings.Join(names, ",")
}

// DefaultCWD returns an absolute working directory for prompt requests.
func DefaultCWD(cwd string) (string, error) {
	if cwd == "" {
		return filepath.Abs(".")
	}

	return filepath.Abs(cwd)
}
