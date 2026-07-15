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
	registry := tool.NewRegistry(cwd)
	if isNilToolProvider(provider) {
		return registry, nil
	}

	if err := registerExtensionTools(registry, provider, provider.Tools()); err != nil {
		return nil, oops.In("assistant").Code("register_extension_tools").Wrapf(err, "register extension tools")
	}

	return registry, nil
}

func (runtime *Runtime) promptToolRegistry(cwd, sessionID string) (*tool.Registry, error) {
	if runtime.profile.Kind != ExecutionTopLevel {
		return runtime.profileToolRegistry(cwd)
	}

	provider := runtime.extensions
	if !runtime.profile.EnableExtensions {
		provider = nil
	}

	registry, err := newToolRegistry(cwd, provider)
	if err != nil {
		return nil, err
	}

	if runtime.agents != nil && runtime.agentTasks != nil && runtime.agents.Len() > 0 {
		for _, name := range []tool.Name{
			agentStartToolName,
			agentStatusToolName,
			agentWaitToolName,
			agentCancelToolName,
			agentListToolName,
		} {
			executor := &agentToolExecutor{
				controller:      runtime.agentTasks,
				sessions:        runtime.sessions,
				catalog:         runtime.agents,
				name:            name,
				parentSessionID: sessionID,
				cwd:             cwd,
				definition:      nil,
			}
			if err := registry.Register(executor); err != nil {
				return nil, oops.In("assistant").Code("register_agent_tool").Wrapf(err, "register agent tool")
			}
		}
	}

	invoke := func(
		ctx context.Context,
		name string,
		arguments tool.Arguments,
		argumentsJSON string,
	) (tool.Result, ToolEvent) {
		return runtime.invokeNestedTool(ctx, registry, name, arguments, argumentsJSON)
	}
	if err := registry.Register(newExecuteTool(registry, invoke)); err != nil {
		return nil, oops.In("assistant").Code("register_execute_tool").Wrapf(err, "register execute tool")
	}

	return registry, nil
}

func (runtime *Runtime) profileToolRegistry(cwd string) (*tool.Registry, error) {
	registry, err := tool.NewRegistryWithTools(cwd, runtime.profile.Tools)
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
