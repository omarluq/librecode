package tool

import (
	"context"
	"errors"
	"sort"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

// ErrUnknownTool is returned when a registry cannot resolve a tool name.
var ErrUnknownTool = errors.New("tool: unknown tool")

// ErrDuplicateTool is returned when a registry already has an executor for a tool name.
var ErrDuplicateTool = errors.New("tool: duplicate tool")

// Registry owns built-in tool executors for a working directory.
type Registry struct {
	schemaValidators *schemaValidatorCache
	executors        map[Name]Executor
	cwd              string
	allowMutations   bool
}

// NewRegistry creates a registry with every built-in tool enabled.
func NewRegistry(cwd string) *Registry {
	registry, err := NewRegistryWithTools(cwd, builtInToolNames())
	if err != nil {
		panic(err)
	}

	return registry
}

// NewRegistryWithTools creates a registry containing only the named built-in tools.
func NewRegistryWithTools(cwd string, names []Name) (*Registry, error) {
	factories := builtInToolFactories(cwd)
	registry := &Registry{
		schemaValidators: newSchemaValidatorCache(),
		executors:        make(map[Name]Executor, len(names)),
		cwd:              cwd,
		allowMutations:   true,
	}

	for _, name := range names {
		if _, exists := registry.executors[name]; exists {
			continue
		}

		factory, ok := factories[name]
		if !ok {
			return nil, oops.In("tool").Code("unknown_tool").With("tool", name).Wrapf(ErrUnknownTool, "create registry")
		}

		registry.executors[name] = factory()
	}

	return registry, nil
}

func builtInToolNames() []Name {
	return []Name{NameRead, NameBash, NameEdit, NameWrite, NameGrep, NameFind, NameLS, NameAST, NameFetch}
}

func builtInToolFactories(cwd string) map[Name]func() Executor {
	return map[Name]func() Executor{
		NameRead:  func() Executor { return NewReadTool(cwd) },
		NameBash:  func() Executor { return NewBashTool(cwd) },
		NameEdit:  func() Executor { return NewEditTool(cwd) },
		NameWrite: func() Executor { return NewWriteTool(cwd) },
		NameGrep:  func() Executor { return NewGrepTool(cwd) },
		NameFind:  func() Executor { return NewFindTool(cwd) },
		NameLS:    func() Executor { return NewLSTool(cwd) },
		NameAST:   func() Executor { return NewASTTool(cwd) },
		NameFetch: func() Executor { return NewFetchTool() },
	}
}

// DenyMutations makes the registry reject tools not marked read-only.
// It is intended for non-interactive background executions that cannot ask for approval.
func (registry *Registry) DenyMutations() {
	registry.allowMutations = false
}

// CWD returns the registry working directory.
func (registry *Registry) CWD() string {
	return registry.cwd
}

// Register adds a tool executor to the registry.
func (registry *Registry) Register(executor Executor) error {
	definition := executor.Definition()
	if _, exists := registry.executors[definition.Name]; exists {
		return oops.
			In("tool").
			Code("duplicate_tool").
			With("tool", definition.Name).
			Wrapf(ErrDuplicateTool, "register tool")
	}

	registry.executors[definition.Name] = executor

	return nil
}

// Definitions returns sorted tool definitions.
func (registry *Registry) Definitions() []Definition {
	definitions := lo.Map(lo.Values(registry.executors), func(executor Executor, _ int) Definition {
		return executor.Definition()
	})
	sort.Slice(definitions, func(leftIndex, rightIndex int) bool {
		return definitions[leftIndex].Name < definitions[rightIndex].Name
	})

	return definitions
}

// Execute runs a named tool with raw JSON object arguments.
func (registry *Registry) Execute(ctx context.Context, name string, input Arguments) (Result, error) {
	executor, ok := registry.executors[Name(name)]
	if !ok {
		return emptyToolResult(), oops.
			In("tool").
			Code("unknown_tool").
			With("tool", name).
			Wrapf(ErrUnknownTool, "resolve tool")
	}

	definition := executor.Definition()
	if !registry.allowMutations && !definition.ReadOnly {
		return emptyToolResult(), oops.In("tool").Code("mutation_denied").
			With("tool", name).Errorf("mutating tool is denied by execution policy")
	}

	if err := validateToolInput(&definition, input, registry.schemaValidators); err != nil {
		return emptyToolResult(), err
	}

	result, err := executor.Execute(ctx, input)

	return result, toolWrap(err, "execute tool")
}

// ExecuteJSON runs a named tool with raw JSON object arguments.
func (registry *Registry) ExecuteJSON(ctx context.Context, name string, payload []byte) (Result, error) {
	input, err := ArgumentsFromRaw(payload)
	if err != nil {
		return emptyToolResult(), err
	}

	return registry.Execute(ctx, name, input)
}

// AllDefinitions returns the definitions for all built-in tools.
func AllDefinitions() []Definition {
	return NewRegistry("").Definitions()
}

func decodeInput(input Arguments, decoded any) error {
	return input.Decode(decoded)
}
