package assistant

import (
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"log/slog"
)

// runtimeDeps is a bag of optional Runtime dependencies used by internal test
// factories. Every field is nil by default; callers set only what they need.
type runtimeDeps struct {
	Config     *config.Config
	Sessions   *database.SessionRepository
	Extensions runtimeExtensions
	Cache      *ResponseCache
	Events     *event.Bus
	Models     *model.Registry
	Client     Completer
	Logger     *slog.Logger
}

// newRuntimeFromDeps builds a Runtime with every field explicitly set, satisfying
// exhaustruct while centralizing the struct literal in a single place.
func newRuntimeFromDeps(setup func(*runtimeDeps)) *Runtime {
	deps := &runtimeDeps{
		Config:     nil,
		Sessions:   nil,
		Extensions: nil,
		Cache:      nil,
		Events:     nil,
		Models:     nil,
		Client:     nil,
		Logger:     nil,
	}
	if setup != nil {
		setup(deps)
	}

	client := deps.Client
	if client == nil {
		client = NewHTTPClient()
	}

	return &Runtime{
		cfg:         deps.Config,
		sessions:    deps.Sessions,
		extensions:  deps.Extensions,
		cache:       deps.Cache,
		events:      deps.Events,
		models:      deps.Models,
		client:      client,
		logger:      deps.Logger,
		skillsCache: nil,
	}
}

func providerHookTestInput(payload map[string]any, headers map[string]string, attempt int) *llm.HookInput {
	request := providerHookTestRequest()

	return &llm.HookInput{
		ProviderOptions: map[string]any{"cwd": request.CWD},
		Payload:         payload,
		Headers:         headers,
		SessionID:       request.SessionID,
		ThinkingLevel:   request.ThinkingLevel,
		Model:           llmModelRefFromModel(&request.Model),
		Attempt:         attempt,
	}
}
