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
}

// NewRegistry creates a registry with every built-in tool enabled.
func NewRegistry(cwd string) *Registry {
	executors := []Executor{
		NewReadTool(cwd),
		NewBashTool(cwd),
		NewEditTool(cwd),
		NewWriteTool(cwd),
		NewGrepTool(cwd),
		NewFindTool(cwd),
		NewLSTool(cwd),
		NewASTTool(cwd),
		NewFetchTool(),
	}

	registry := &Registry{
		schemaValidators: newSchemaValidatorCache(),
		executors:        make(map[Name]Executor, len(executors)),
		cwd:              cwd,
	}
	for _, executor := range executors {
		definition := executor.Definition()
		registry.executors[definition.Name] = executor
	}

	return registry
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
