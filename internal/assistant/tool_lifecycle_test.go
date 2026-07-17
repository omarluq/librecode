package assistant_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/testutil"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	testToolLifecycleError    = "boom"
	testToolLifecycleParentID = "parent-call"
	testToolLifecycleSequence = 7
)

func TestRuntime_ToolCallLifecycleAppliesArgumentMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		lua                   string
		expectedArgumentsJSON string
		expectedArguments     tool.Arguments
		initialArguments      tool.Arguments
	}{
		{
			name:             "rewrites path and adds limit",
			initialArguments: testutil.ToolArguments(map[string]any{testToolPathKey: testToolPath}),
			lua: `
local lc = require("librecode")
lc.on("tool_call", function(event)
  assert(event.payload.call_id == "call-1")
  assert(event.payload.parent_call_id == "parent-call")
  assert(event.payload.sequence == 7)
  return {
    tool_call = {
      arguments = {
        path = "changed.txt",
        limit = 3,
      },
    },
  }
end)
`,
			expectedArguments: testutil.ToolArguments(map[string]any{
				testToolPathKey: "changed.txt",
				"limit":         float64(3),
			}),
			expectedArgumentsJSON: `{"limit":3,"path":"changed.txt"}`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtime, _, manager := newTestRuntimeWithManager(t, testCompleter{})
			loadRuntimeExtension(t, manager, testCase.lua)
			call := assistant.ToolCallEvent{
				ParentCallID: testToolLifecycleParentID,
				Sequence:     testToolLifecycleSequence,

				Arguments:     testCase.initialArguments,
				ID:            testToolCallID,
				Name:          testToolName,
				ArgumentsJSON: testToolArgsJSON,
			}

			err := runtime.DispatchToolCallLifecycleForTest(context.Background(), &call)

			require.NoError(t, err)
			assert.Equal(
				t,
				testutil.ToolArgumentFields(testCase.expectedArguments),
				testutil.ToolArgumentFields(call.Arguments),
			)
			assert.JSONEq(t, testCase.expectedArgumentsJSON, call.ArgumentsJSON)
			assert.Equal(t, testToolCallID, call.ID)
			assert.Equal(t, testToolLifecycleParentID, call.ParentCallID)
			assert.Equal(t, testToolLifecycleSequence, call.Sequence)
		})
	}
}

func TestRuntime_ToolResultLifecycleAppliesResultMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		initialEvent        *assistant.ToolEvent
		name                string
		lua                 string
		expectedResult      string
		expectedDetailsJSON string
		expectedError       string
	}{
		{
			name: "redacts result and clears error",
			initialEvent: &assistant.ToolEvent{
				CallID:       testToolCallID,
				ParentCallID: testToolLifecycleParentID,
				Sequence:     testToolLifecycleSequence,

				Name:          testToolName,
				ArgumentsJSON: testToolArgsJSON,
				DetailsJSON:   "{}",
				Result:        "secret",
				Error:         testToolLifecycleError,
				IsError:       true,
			},
			lua: `
local lc = require("librecode")
lc.on("tool_result", function(event)
  assert(event.payload.call_id == "call-1")
  assert(event.payload.parent_call_id == "parent-call")
  assert(event.payload.sequence == 7)
  return {
    tool_result = {
      result = "redacted",
      details_json = "{\"redacted\":true}",
      error = "",
    },
  }
end)
`,
			expectedResult:      "redacted",
			expectedDetailsJSON: `{"redacted":true}`,
			expectedError:       "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtime, _, manager := newTestRuntimeWithManager(t, testCompleter{})
			loadRuntimeExtension(t, manager, testCase.lua)

			err := runtime.DispatchToolResultLifecycleForTest(context.Background(), testCase.initialEvent)

			require.NoError(t, err)
			assert.Equal(t, testCase.expectedResult, testCase.initialEvent.Result)
			assert.JSONEq(t, testCase.expectedDetailsJSON, testCase.initialEvent.DetailsJSON)
			assert.Equal(t, testCase.expectedError, testCase.initialEvent.Error)
			assert.Equal(t, testToolCallID, testCase.initialEvent.CallID)
			assert.Equal(t, testToolLifecycleParentID, testCase.initialEvent.ParentCallID)
			assert.Equal(t, testToolLifecycleSequence, testCase.initialEvent.Sequence)
		})
	}
}

func TestRuntime_ToolLifecycleReturnsHandlerErrors(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompleter{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("tool_call", function()
  error("tool hook failed")
end)
lc.on("tool_result", function()
  error("tool result hook failed")
end)
`)

	call := assistant.ToolCallEvent{
		ParentCallID: "",
		Sequence:     0,

		Arguments:     testutil.ToolArguments(map[string]any{testToolPathKey: testToolPath}),
		ID:            testToolCallID,
		Name:          testToolName,
		ArgumentsJSON: testToolArgsJSON,
	}
	event := &assistant.ToolEvent{
		CallID:       "",
		ParentCallID: "",
		Sequence:     0,

		Name:          testToolName,
		ArgumentsJSON: testToolArgsJSON,
		DetailsJSON:   "{}",
		Result:        testToolResult,
		Error:         "",
		IsError:       false,
	}

	callErr := runtime.DispatchToolCallLifecycleForTest(context.Background(), &call)
	resultErr := runtime.DispatchToolResultLifecycleForTest(context.Background(), event)

	require.Error(t, callErr)
	require.Error(t, resultErr)
	assert.Contains(t, callErr.Error(), "lifecycle handlers failed")
	assert.Contains(t, resultErr.Error(), "lifecycle handlers failed")
}

func TestRuntime_ToolResultLifecycleDispatchesToolErrorHandlers(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompleter{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
local seen = ""
lc.on("tool_error", function(event)
  seen = event.payload.name .. ":" .. event.payload.error
end)
lc.register_command("seen_tool_error", "seen_tool_error", function()
  return seen
end)
`)

	event := &assistant.ToolEvent{
		CallID:       "",
		ParentCallID: "",
		Sequence:     0,

		Name:          testToolName,
		ArgumentsJSON: testToolArgsJSON,
		DetailsJSON:   "",
		Result:        testToolLifecycleError,
		Error:         testToolLifecycleError,
		IsError:       true,
	}

	err := runtime.DispatchToolResultLifecycleForTest(context.Background(), event)

	require.NoError(t, err)
	output, err := manager.ExecuteCommand(context.Background(), "seen_tool_error", "")
	require.NoError(t, err)
	assert.Equal(t, "read:boom", output)
}
