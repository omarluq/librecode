package extension

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/omarluq/librecode/internal/testutil"
	"github.com/omarluq/librecode/internal/tool"
)

func TestMergeToolCallMutationBranches(t *testing.T) {
	t.Parallel()

	base := ToolCallMutation{
		Arguments: testutil.ToolArguments(map[string]any{"path": "README.md"}),
		HasArgs:   true,
	}

	tests := []struct { //nolint:govet // Table-driven tests prefer readable field order over fieldalignment.
		name     string
		override ToolCallMutation
		wantJSON string
		wantArgs bool
	}{
		{
			name:     "keeps base when override has no arguments",
			override: ToolCallMutation{Arguments: tool.EmptyArguments(), HasArgs: false},
			wantJSON: `{"path":"README.md"}`,
			wantArgs: true,
		},
		{
			name:     "explicit empty override clears arguments",
			override: ToolCallMutation{Arguments: tool.EmptyArguments(), HasArgs: true},
			wantJSON: `{}`,
			wantArgs: true,
		},
		{
			name: "merges override fields into base",
			override: ToolCallMutation{
				Arguments: testutil.ToolArguments(map[string]any{"limit": 5}),
				HasArgs:   true,
			},
			wantJSON: `{"limit":5,"path":"README.md"}`,
			wantArgs: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := mergeToolCallMutation(base, testCase.override)

			assert.Equal(t, testCase.wantArgs, got.HasArgs)
			assert.JSONEq(t, testCase.wantJSON, got.Arguments.String())
		})
	}
}

func TestToolCallMutationFromLuaFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func(*lua.LState) lua.LValue
		name  string
	}{
		{name: "non table", build: func(*lua.LState) lua.LValue { return lua.LString("nope") }},
		{name: "missing arguments", build: func(state *lua.LState) lua.LValue { return state.NewTable() }},
		{
			name:  "non table arguments",
			build: func(state *lua.LState) lua.LValue { return toolCallMutationTable(state, lua.LString("nope")) },
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			state := lua.NewState()
			t.Cleanup(state.Close)

			mutation, ok := toolCallMutationFromLua(testCase.build(state))

			assert.False(t, ok)
			assert.False(t, mutation.HasArgs)
			assert.True(t, mutation.Arguments.IsEmpty())
		})
	}
}

func TestToolCallMutationFromLuaAcceptsExplicitEmptyArguments(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	mutation, ok := toolCallMutationFromLua(toolCallMutationTable(state, state.NewTable()))
	require.True(t, ok)
	assert.True(t, mutation.HasArgs)
	assert.JSONEq(t, `{}`, mutation.Arguments.String())
}

func toolCallMutationTable(state *lua.LState, arguments lua.LValue) *lua.LTable {
	table := state.NewTable()
	state.SetField(table, "arguments", arguments)

	return table
}

func TestLifecycleMutationHelpersFallbacks(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	result := LifecycleDispatchResult{
		ToolResult:      ToolResultMutation{Result: nil, DetailsJSON: nil, Error: nil},
		Payload:         nil,
		ProviderRequest: ProviderRequestMutation{Headers: nil},
		Compaction:      CompactionMutation{Summary: nil, FirstKeptEntryID: nil, Details: nil, Cancel: false},
		Name:            "",
		Errors:          nil,
		ToolCall:        ToolCallMutation{Arguments: tool.EmptyArguments(), HasArgs: false},
		Duration:        0,
		HandlerCount:    0,
		Consumed:        false,
		Stopped:         false,
	}
	applyLifecycleLuaResult(&result, lua.LString("ignored"))
	assert.False(t, result.Consumed)

	handled := state.NewTable()
	state.SetField(handled, "handled", lua.LBool(true))
	applyLifecycleControlResult(&result, handled)
	assert.True(t, result.Consumed)

	toolResult, matched := toolResultMutationFromLua(state.NewTable())
	assert.False(t, matched)
	assert.Nil(t, toolResult.Result)

	compaction, matched := compactionMutationFromLua(state.NewTable())
	assert.False(t, matched)
	assert.False(t, compaction.Cancel)

	providerRequest, matched := providerRequestMutationFromLua(lua.LString("nope"))
	assert.False(t, matched)
	assert.Empty(t, providerRequest.Headers)

	assert.Empty(t, stringMapValue("nope"))
	assert.Equal(t, map[string]string{"ok": "y"}, stringMapValue(map[string]any{"ok": "y", "skip": 1}))
}

func TestDispatchLifecycleReturnsContextErrorBeforeHandler(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil)
	state := lua.NewState()
	t.Cleanup(state.Close)
	manager.handlers[string(LifecycleInput)] = []luaHookHandler{{
		extension: &luaExtension{
			activeEvent:   nil,
			state:         state,
			name:          "test",
			path:          "test.lua",
			commands:      nil,
			tools:         nil,
			keymaps:       nil,
			handlers:      nil,
			lock:          sync.Mutex{},
			totalDuration: atomic.Int64{},
		},
		function: state.NewFunction(func(*lua.LState) int { return 0 }),
		priority: 0,
		order:    0,
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := manager.DispatchLifecycle(ctx, LifecycleEvent{Name: LifecycleInput, Payload: nil})

	require.Error(t, err)
	assert.Zero(t, result.HandlerCount)
}
