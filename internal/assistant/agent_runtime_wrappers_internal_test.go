package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
)

type testToolProvider struct {
	tools []extension.Tool
}

func (provider testToolProvider) Tools() []extension.Tool { return provider.tools }
func (testToolProvider) ExecuteCommand(context.Context, string, string) (string, error) {
	return "", nil
}
func (testToolProvider) Emit(context.Context, string, map[string]any) error { return nil }
func (testToolProvider) DispatchLifecycle(
	_ context.Context,
	event extension.LifecycleEvent,
) (extension.LifecycleDispatchResult, error) {
	return emptyTestLifecycleDispatchResult(event), nil
}
func (testToolProvider) ExecuteTool(context.Context, string, tool.Arguments) (extension.ToolResult, error) {
	return extension.ToolResult{Details: nil, Content: "ok"}, nil
}

type pointerToolProvider struct{}

func (*pointerToolProvider) Tools() []extension.Tool { return nil }
func (*pointerToolProvider) ExecuteTool(context.Context, string, tool.Arguments) (extension.ToolResult, error) {
	return extension.ToolResult{Details: nil, Content: ""}, nil
}

func TestRuntimeAgentWrappersWithoutController(t *testing.T) {
	t.Parallel()

	var nilRuntime *Runtime
	for _, testCase := range []struct {
		runtime *Runtime
		name    string
	}{
		{name: "nil runtime", runtime: nilRuntime},
		{name: "runtime without controller", runtime: new(Runtime)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtime := testCase.runtime
			assert.Nil(t, runtime.AgentDefinitions())
			assert.Nil(t, runtime.AgentDiagnostics())
			tasks, err := runtime.AgentTasks(t.Context(), "owner", 10)
			require.NoError(t, err)
			assert.Nil(t, tasks)
			task, found, err := runtime.AgentTask(t.Context(), "id")
			require.NoError(t, err)
			assert.Nil(t, task)
			assert.False(t, found)
			canceled, found, err := runtime.CancelAgentTask(t.Context(), "owner", "id")
			require.NoError(t, err)
			assert.Nil(t, canceled)
			assert.False(t, found)

			events, cancel := runtime.SubscribeAgentTask("id")
			cancel()

			_, open := <-events
			assert.False(t, open)
		})
	}
}

func TestRuntimeAgentControllerSetterAndSubscriptionDelegate(t *testing.T) {
	t.Parallel()

	stub := new(agentControllerStub)
	runtime := new(Runtime)
	runtime.SetAgentTaskController(stub)
	events, cancel := runtime.SubscribeAgentTask("task")
	cancel()

	_, open := <-events
	assert.False(t, open)
	assert.Equal(t, 1, stub.subscriptions)
}

func TestRuntimeAgentWrappersDelegateAndWrapErrors(t *testing.T) {
	t.Parallel()

	stub := new(agentControllerStub)
	stub.task = agentToolTask("id", "owner", database.TaskQueued)
	stub.listed = []database.AgentTaskEntity{}
	stub.found = true
	runtime := new(Runtime)
	runtime.agentTasks = stub
	runtime.agents = agent.Load(t.TempDir())
	assert.NotEmpty(t, runtime.AgentDefinitions())
	assert.Empty(t, runtime.AgentDiagnostics())
	_, err := runtime.AgentTasks(t.Context(), "owner", 7)
	require.NoError(t, err)
	assert.Equal(t, 7, stub.lastLimit)
	task, found, err := runtime.AgentTask(t.Context(), "id")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "id", task.Task.ID)
	_, found, err = runtime.CancelAgentTask(t.Context(), "owner", "id")
	require.NoError(t, err)
	assert.True(t, found)

	stub.listErr = errors.New("list failed")
	_, err = runtime.AgentTasks(t.Context(), "owner", 1)
	require.ErrorContains(t, err, "list agent tasks")

	stub.getErr = errors.New("get failed")
	_, _, err = runtime.AgentTask(t.Context(), "id")
	require.ErrorContains(t, err, "get agent task")

	stub.cancelErr = errors.New("cancel failed")
	_, _, err = runtime.CancelAgentTask(t.Context(), "owner", "id")
	require.ErrorContains(t, err, "cancel agent task")
}

func TestAsyncAgentPromptAndToolRegistries(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	catalog := agent.Load(cwd)
	stub := new(agentControllerStub)
	sessions := agentToolSessions(t)
	runtime := new(Runtime)
	runtime.agents = catalog
	runtime.agentTasks = stub
	runtime.sessions = sessions
	runtime.profile = topLevelExecutionProfile()

	prompt := runtime.baseSystemPrompt(cwd)
	assert.Contains(t, prompt, "Use agent_start")
	assert.Contains(t, prompt, "agent_cancel")

	registry, err := runtime.promptToolRegistry(cwd, "owner")
	require.NoError(t, err)

	names := make([]tool.Name, 0, len(registry.Definitions()))
	for _, definition := range registry.Definitions() {
		names = append(names, definition.Name)
	}

	for _, name := range []tool.Name{
		agentStartToolName, agentStatusToolName, agentWaitToolName, agentCancelToolName, agentListToolName,
	} {
		assert.Contains(t, names, name)
	}

	runtime.agentTasks = nil
	registry, err = runtime.promptToolRegistry(cwd, "owner")
	require.NoError(t, err)

	for _, definition := range registry.Definitions() {
		assert.False(t, strings.HasPrefix(string(definition.Name), "agent_"))
	}
}

func TestAgentExecutionProfileRegistryAndPrompt(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	runtime := new(Runtime)
	runtime.profile = topLevelExecutionProfile()
	runtime.profile.Kind = ExecutionAgentTask
	runtime.profile.SystemPrompt = "You are focused."
	runtime.profile.Tools = []tool.Name{tool.NameRead, tool.NameWrite}
	runtime.profile.PermissionMode = agent.PermissionDeny
	prompt := runtime.baseSystemPrompt(cwd)
	assert.Contains(t, prompt, "You are focused.")
	assert.Contains(t, prompt, "Use only the available tools (read, write)")
	assert.NotContains(t, prompt, "Use agent_start")

	registry, err := runtime.promptToolRegistry(cwd, "ignored")
	require.NoError(t, err)
	_, err = registry.Execute(t.Context(), string(tool.NameWrite), agentArguments(t, `{"path":"x","content":"y"}`))
	require.ErrorContains(t, err, "mutating tool is denied")

	runtime.profile.PermissionMode = agent.PermissionAllow
	registry, err = runtime.profileToolRegistry(cwd)
	require.NoError(t, err)
	assert.Len(t, registry.Definitions(), 2)

	runtime.profile.Tools = []tool.Name{"not-a-tool"}
	_, err = runtime.profileToolRegistry(cwd)
	require.ErrorContains(t, err, "create child tool registry")
}

func TestToolRegistryProviderAndCollisionBranches(t *testing.T) {
	t.Parallel()

	const customToolName = "custom"

	assert.True(t, isNilToolProvider(nil))

	var typedNil *pointerToolProvider
	assert.True(t, isNilToolProvider(typedNil))
	assert.False(t, isNilToolProvider(&pointerToolProvider{}))
	assert.False(t, isNilToolProvider(testToolProvider{tools: nil}))

	provider := testToolProvider{tools: []extension.Tool{{
		Name: customToolName, Description: "custom tool", Extension: "", InputSchema: tool.EmptySchema(),
	}}}
	registry, err := newToolRegistry(t.TempDir(), provider)
	require.NoError(t, err)
	result, err := registry.Execute(t.Context(), customToolName, tool.EmptyArguments())
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Text())

	provider.tools = []extension.Tool{{
		Name: "read", Description: "collision", Extension: "", InputSchema: tool.EmptySchema(),
	}}
	_, err = newToolRegistry(t.TempDir(), provider)
	require.ErrorContains(t, err, "register extension tools")

	runtime := new(Runtime)
	runtime.extensions = provider
	runtime.agents = agent.Load(t.TempDir())
	runtime.agentTasks = new(agentControllerStub)
	runtime.profile = topLevelExecutionProfile()
	runtime.extensions = testToolProvider{tools: []extension.Tool{{
		Name: string(agentStartToolName), Description: "collision", Extension: "", InputSchema: tool.EmptySchema(),
	}}}
	_, err = runtime.promptToolRegistry(t.TempDir(), "owner")
	require.ErrorContains(t, err, "register agent tool")
}

func TestPromptRegistryHonorsDisabledExtensions(t *testing.T) {
	t.Parallel()

	runtime := new(Runtime)
	runtime.extensions = testToolProvider{tools: []extension.Tool{{
		Name: "custom", Description: "custom", Extension: "", InputSchema: tool.EmptySchema(),
	}}}
	runtime.profile = topLevelExecutionProfile()
	runtime.profile.EnableExtensions = false
	registry, err := runtime.promptToolRegistry(t.TempDir(), "owner")
	require.NoError(t, err)

	for _, definition := range registry.Definitions() {
		assert.NotEqual(t, tool.Name("custom"), definition.Name)
	}
}

func TestWithExecutionProfileClonesToolsAndDependencies(t *testing.T) {
	t.Parallel()

	runtime := new(Runtime)
	runtime.agents = agent.Load(t.TempDir())
	profile := &ExecutionProfile{
		Kind: ExecutionAgentTask, AgentName: "general", SystemPrompt: "prompt", Provider: "", Model: "",
		ThinkingLevel: "", PermissionMode: agent.PermissionDeny, Tools: []tool.Name{tool.NameRead},
		EnableSkills: false, EnableExtensions: false, MaxTurns: 2, Depth: 1,
	}
	clone := runtime.WithExecutionProfile(profile)
	profile.Tools[0] = tool.NameWrite
	assert.Equal(t, tool.NameRead, clone.profile.Tools[0])
	assert.Same(t, runtime.agents, clone.agents)
	assert.NotSame(t, runtime, clone)
}
