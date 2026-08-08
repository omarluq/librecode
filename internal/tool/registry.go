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
	return NewRegistryWithCoordinator(cwd, names, NewCoordinator())
}

// NewRegistryWithCoordinator creates a registry sharing process-wide mutation coordination.
func NewRegistryWithCoordinator(cwd string, names []Name, coordinator *Coordinator) (*Registry, error) {
	if coordinator == nil {
		coordinator = NewCoordinator()
	}

	factories := builtInToolFactories(cwd)
	registry := &Registry{
		schemaValidators: newSchemaValidatorCache(),
		executors:        make(map[Name]Executor, len(names)),
		cwd:              cwd,
		allowMutations:   true,
	}

	mutationLocks := coordinator.files
	factories[NameEdit] = func() Executor { return newEditTool(cwd, mutationLocks) }
	factories[NameWrite] = func() Executor { return newWriteTool(cwd, mutationLocks) }
	factories[NameBash] = func() Executor { return newBashTool(cwd, mutationLocks) }

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

// Wrap replaces an executor with a decorator that preserves its definition name.
func (registry *Registry) Wrap(name Name, decorate func(Executor) Executor) error {
	if registry == nil || decorate == nil {
		return oops.In("tool").Code("unknown_tool").With("tool", name).
			Wrapf(ErrUnknownTool, "wrap tool")
	}

	executor := registry.executors[name]
	if executor == nil {
		return oops.In("tool").Code("unknown_tool").With("tool", name).
			Wrapf(ErrUnknownTool, "wrap tool")
	}

	wrapped := decorate(executor)
	if wrapped == nil || wrapped.Definition().Name != name {
		return oops.In("tool").Code("invalid_tool_wrapper").With("tool", name).
			Errorf("wrapped tool must preserve its name")
	}

	registry.executors[name] = wrapped

	return nil
}

// Has reports whether a tool is registered under name.
func (registry *Registry) Has(name Name) bool {
	if registry == nil {
		return false
	}

	_, exists := registry.executors[name]

	return exists
}

type executionFunc func(context.Context) (Result, error)

type preparedExecution struct {
	mutationLocks *fileMutationLocks
	execute       executionFunc
	mutationPath  string
}

type executionPreparer interface {
	prepareExecution(Arguments) (preparedExecution, error)
}

type sequentialToolExecutor interface {
	Sequential() bool
}

// PreparedCall is a validated tool invocation ready for execution.
// A call must not be used by multiple goroutines concurrently. Batch callers may
// Admit in source order, Execute on a worker, then Release after execution completes.
type PreparedCall struct {
	mutationLocks *fileMutationLocks
	reservation   *fileMutationReservation
	execute       executionFunc
	mutationPath  string
	sequential    bool
}

// Sequential reports whether this call requires the whole batch to run in source order.
func (call PreparedCall) Sequential() bool {
	return call.sequential
}

// Admit reserves any source-ordered resources needed by the call.
// Callers preparing a batch must admit calls in source order before executing them concurrently.
func (call *PreparedCall) Admit() {
	if call == nil || call.mutationLocks == nil {
		return
	}

	call.reservation = call.mutationLocks.reserve(call.mutationPath)
	call.mutationLocks = nil
	call.mutationPath = ""
}

// TryAdmit reserves resources only when they are immediately available.
// A false result leaves the call prepared and may be retried later.
func (call *PreparedCall) TryAdmit() bool {
	if call == nil || call.mutationLocks == nil {
		return true
	}

	reservation, ok := call.mutationLocks.tryReserve(call.mutationPath)
	if !ok {
		return false
	}

	call.reservation = reservation
	call.mutationLocks = nil
	call.mutationPath = ""

	return true
}

// Release abandons resources reserved by Admit. It is safe to call after Execute.
func (call *PreparedCall) Release() {
	if call == nil || call.reservation == nil {
		return
	}

	call.reservation.remove()
	call.reservation = nil
}

// Execute runs the prepared call.
func (call *PreparedCall) Execute(ctx context.Context) (Result, error) {
	if call == nil || call.execute == nil {
		return emptyToolResult(), oops.In("tool").Code("call_not_prepared").
			Errorf("prepared call is not initialized")
	}

	call.Admit()
	execute := call.execute

	call.execute = nil
	if call.reservation == nil {
		return execute(ctx)
	}

	reservation := call.reservation
	call.reservation = nil

	return reservation.execute(ctx, func() (Result, error) {
		return execute(ctx)
	})
}

// Prepare resolves and validates a named tool invocation without executing it.
func (registry *Registry) Prepare(name string, input Arguments) (PreparedCall, error) {
	var executor Executor
	if registry != nil {
		executor = registry.executors[Name(name)]
	}

	if executor == nil {
		return PreparedCall{}, oops.
			In("tool").
			Code("unknown_tool").
			With("tool", name).
			Wrapf(ErrUnknownTool, "resolve tool")
	}

	definition := executor.Definition()
	if !registry.allowMutations && !definition.ReadOnly {
		return PreparedCall{}, oops.In("tool").Code("mutation_denied").
			With("tool", name).Errorf("mutating tool is denied by execution policy")
	}

	if err := validateToolInput(&definition, input, registry.schemaValidators); err != nil {
		return PreparedCall{}, err
	}

	prepared := preparedExecution{
		mutationLocks: nil,
		execute: func(ctx context.Context) (Result, error) {
			return executor.Execute(ctx, input)
		},
		mutationPath: "",
	}

	if preparer, ok := executor.(executionPreparer); ok {
		var err error

		prepared, err = preparer.prepareExecution(input)
		if err != nil {
			return PreparedCall{}, err
		}
	}

	sequentialExecutor, hasSequentialMetadata := executor.(sequentialToolExecutor)

	return PreparedCall{
		execute: func(ctx context.Context) (Result, error) {
			result, executeErr := prepared.execute(ctx)

			return result, toolWrap(executeErr, "execute tool")
		},
		mutationPath:  prepared.mutationPath,
		mutationLocks: prepared.mutationLocks,
		reservation:   nil,
		sequential:    hasSequentialMetadata && sequentialExecutor.Sequential(),
	}, nil
}

// Execute runs a named tool with raw JSON object arguments.
func (registry *Registry) Execute(ctx context.Context, name string, input Arguments) (Result, error) {
	call, err := registry.Prepare(name, input)
	if err != nil {
		return emptyToolResult(), err
	}

	return (&call).Execute(ctx)
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
