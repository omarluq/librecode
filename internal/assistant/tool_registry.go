package assistant

import (
	"context"
	"reflect"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/agent"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
)

func nilableToolProviderKinds() map[reflect.Kind]struct{} {
	return map[reflect.Kind]struct{}{
		reflect.Chan:      {},
		reflect.Func:      {},
		reflect.Interface: {},
		reflect.Map:       {},
		reflect.Pointer:   {},
		reflect.Slice:     {},
	}
}

type toolProvider interface {
	Tools() []extension.Tool
	extensionToolRunner
}

func newToolRegistry(cwd string, provider toolProvider) (*tool.Registry, error) {
	return newToolRegistryWithCoordinator(cwd, provider, nil)
}

func newToolRegistryWithCoordinator(
	cwd string,
	provider toolProvider,
	coordinator *tool.Coordinator,
) (*tool.Registry, error) {
	names := []tool.Name{
		tool.NameRead, tool.NameBash, tool.NameEdit, tool.NameWrite, tool.NameGrep,
		tool.NameFind, tool.NameLS, tool.NameAST, tool.NameFetch,
	}

	registry, err := tool.NewRegistryWithCoordinator(cwd, names, coordinator)
	if err != nil {
		return nil, oops.In("assistant").Code("create_tool_registry").Wrapf(err, "create tool registry")
	}

	if isNilToolProvider(provider) {
		return registry, nil
	}

	if err := registerExtensionTools(registry, provider, provider.Tools()); err != nil {
		return nil, oops.In("assistant").Code("register_extension_tools").Wrapf(err, "register extension tools")
	}

	return registry, nil
}

func (runtime *Runtime) promptToolRegistry(
	ctx context.Context,
	cwd string,
	sessionID string,
) (*tool.Registry, error) {
	if runtime.profile.Kind != ExecutionTopLevel {
		return runtime.profileToolRegistry(cwd)
	}

	provider := runtime.extensions
	if !runtime.profile.EnableExtensions {
		provider = nil
	}

	registry, err := newToolRegistryWithCoordinator(cwd, provider, runtime.toolCoordinator)
	if err != nil {
		return nil, err
	}

	if err := runtime.registerAgentTools(registry, sessionID, cwd); err != nil {
		return nil, err
	}

	if err := runtime.registerBackgroundTools(registry, sessionID, cwd); err != nil {
		return nil, err
	}

	if err := runtime.registerExecuteTool(ctx, registry, sessionID); err != nil {
		return nil, err
	}

	return registry, nil
}

func (runtime *Runtime) registerAgentTools(registry *tool.Registry, sessionID, cwd string) error {
	if runtime.agents == nil || runtime.agentTasks == nil || runtime.agents.Len() == 0 {
		return nil
	}

	for _, name := range []tool.Name{
		agentStartToolName, agentStatusToolName, agentWaitToolName, agentCancelToolName, agentListToolName,
	} {
		executor := &agentToolExecutor{
			controller: runtime.agentTasks, sessions: runtime.sessions, catalog: runtime.agents, name: name,
			parentSessionID: sessionID, cwd: cwd, definition: nil,
		}
		if err := registry.Register(executor); err != nil {
			return oops.In("assistant").Code("register_agent_tool").Wrapf(err, "register agent tool")
		}
	}

	return nil
}

func (runtime *Runtime) registerBackgroundTools(registry *tool.Registry, sessionID, cwd string) error {
	if runtime.toolTasks == nil {
		return nil
	}

	for _, name := range []tool.Name{
		tool.NameRead, tool.NameBash, tool.NameEdit, tool.NameWrite,
		tool.NameGrep, tool.NameFind, tool.NameLS, tool.NameAST,
	} {
		err := registry.Wrap(name, func(executor tool.Executor) tool.Executor {
			return &backgroundToolExecutor{
				target: executor, controller: runtime.toolTasks, admit: runtime.admitBackgroundToolTarget,
				cache: new(backgroundDefinitionCache), owner: sessionID, cwd: cwd,
			}
		})
		if err != nil {
			return oops.In("assistant").Code("wrap_background_tool").Wrapf(err, "wrap background tool")
		}
	}

	return nil
}

func (runtime *Runtime) registerExecuteTool(
	ctx context.Context,
	registry *tool.Registry,
	sessionID string,
) error {
	if toolStrategyFromContext(ctx) == ToolStrategyDirect {
		return nil
	}

	invoke := func(
		ctx context.Context,
		name string,
		arguments tool.Arguments,
		argumentsJSON string,
	) (tool.Result, ToolEvent) {
		return runtime.invokeNestedTool(ctx, registry, name, arguments, argumentsJSON)
	}

	executor := newExecuteFacade(registry, invoke, runtime.workflowSubmitter, sessionID)
	if err := registry.Register(executor); err != nil {
		return oops.In("assistant").Code("register_execute_tool").Wrapf(err, "register execute tool")
	}

	return nil
}

func (runtime *Runtime) profileToolRegistry(cwd string) (*tool.Registry, error) {
	registry, err := tool.NewRegistryWithCoordinator(cwd, runtime.profile.Tools, runtime.toolCoordinator)
	if err != nil {
		return nil, oops.In("assistant").Code("create_child_tool_registry").Wrapf(err, "create child tool registry")
	}

	// Background tasks have no interactive approval channel. Inherit and ask
	// therefore fail closed; only an explicit allow policy enables mutations.
	if runtime.profile.PermissionMode != agent.PermissionAllow {
		registry.DenyMutations()
	}

	return registry, nil
}

func isNilToolProvider(provider toolProvider) bool {
	if provider == nil {
		return true
	}

	value := reflect.ValueOf(provider)
	if !value.IsValid() {
		return true
	}

	if _, ok := nilableToolProviderKinds()[value.Kind()]; !ok {
		return false
	}

	return value.IsNil()
}
