package tool

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

// ErrUnknownTool is returned when a registry cannot resolve a tool name.
var ErrUnknownTool = errors.New("tool: unknown tool")

// Registry owns built-in tool executors for a working directory.
type Registry struct {
	executors map[Name]Executor
	cwd       string
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
	}

	return &Registry{
		executors: lo.SliceToMap(executors, func(executor Executor) (Name, Executor) {
			return executor.Definition().Name, executor
		}),
		cwd: cwd,
	}
}

// CWD returns the registry working directory.
func (registry *Registry) CWD() string {
	return registry.cwd
}

// Definitions returns sorted tool definitions.
func (registry *Registry) Definitions() []Definition {
	definitions := lo.Map(lo.Values(registry.executors), func(executor Executor, _ int) Definition {
		return executor.Definition()
	})
	sort.Slice(definitions, func(leftIndex int, rightIndex int) bool {
		return definitions[leftIndex].Name < definitions[rightIndex].Name
	})

	return definitions
}

// Execute runs a named tool with map-shaped JSON arguments.
func (registry *Registry) Execute(ctx context.Context, name string, input map[string]any) (Result, error) {
	executor, ok := registry.executors[Name(name)]
	if !ok {
		return emptyToolResult(), oops.
			In("tool").
			Code("unknown_tool").
			With("tool", name).
			Wrapf(ErrUnknownTool, "resolve tool")
	}

	return executor.Execute(ctx, input)
}

// ExecuteJSON runs a named tool with raw JSON object arguments.
func (registry *Registry) ExecuteJSON(ctx context.Context, name string, payload []byte) (Result, error) {
	input := map[string]any{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &input); err != nil {
			return emptyToolResult(), oops.In("tool").Code("decode_json_input").Wrapf(err, "decode tool input")
		}
	}

	return registry.Execute(ctx, name, input)
}

// AllDefinitions returns the definitions for all built-in tools.
func AllDefinitions() []Definition {
	return NewRegistry("").Definitions()
}

func decodeInput[T any](input map[string]any) (T, error) {
	var decoded T
	payload, err := json.Marshal(input)
	if err != nil {
		return decoded, oops.In("tool").Code("encode_input").Wrapf(err, "encode tool input")
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return decoded, oops.In("tool").Code("decode_input").Wrapf(err, "decode tool input")
	}

	return decoded, nil
}
