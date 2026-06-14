package assistant

import (
	"log/slog"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/model"
)

// RuntimeTestOptions holds optional Runtime dependencies for test factories.
type RuntimeTestOptions struct {
	Config     *config.Config
	Sessions   *database.SessionRepository
	Extensions runtimeExtensions
	Cache      *ResponseCache
	Events     *event.Bus
	Models     *model.Registry
	Client     Completer
	Logger     *slog.Logger
}

// NewRuntimeForTest builds a Runtime from the given setup closure, setting every
// RuntimeOptions field explicitly in a single place. This eliminates duplicated
// struct literals across test files (which triggers SonarCloud duplication
// warnings when exhaustruct forces every field to be listed).
func NewRuntimeForTest(setup func(*RuntimeTestOptions)) *Runtime {
	opts := &RuntimeTestOptions{} //nolint:exhaustruct // intentional zero-init
	if setup != nil {
		setup(opts)
	}

	return NewRuntime(&RuntimeOptions{
		Config:      opts.Config,
		Sessions:    opts.Sessions,
		Extensions:  opts.Extensions,
		Cache:       opts.Cache,
		Events:      opts.Events,
		Models:      opts.Models,
		Client:      opts.Client,
		Logger:      opts.Logger,
		SkillsCache: nil,
	})
}
