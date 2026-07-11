package assistant

import (
	"reflect"

	"github.com/samber/oops"

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
	if runtime.childDefinition != nil {
		registry, err := tool.NewRegistryWithTools(cwd, runtime.childDefinition.Tools)
		if err != nil {
			return nil, oops.In("assistant").Code("create_child_tool_registry").Wrapf(err, "create child tool registry")
		}

		return registry, nil
	}

	registry, err := newToolRegistry(cwd, runtime.extensions)
	if err != nil {
		return nil, err
	}

	if runtime.agents != nil && len(runtime.agents.Definitions()) > 0 {
		executor := &agentToolExecutor{
			runtime: runtime, catalog: runtime.agents, parentSessionID: sessionID, cwd: cwd,
		}
		if err := registry.Register(executor); err != nil {
			return nil, oops.In("assistant").Code("register_agent_tool").Wrapf(err, "register agent tool")
		}
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
