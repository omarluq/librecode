package assistant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/ro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/model"
)

func TestRuntime_PromptEmitsSessionTurnLifecycleEvents(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompletionClient{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
local events = {}
local names = {
  "input",
  "prompt_prepare",
  "session_start",
  "before_agent_start",
  "agent_start",
  "turn_start",
  "message_append",
  "turn_end",
  "agent_end",
}
for _, name in ipairs(names) do
  lc.on(name, function(event)
    table.insert(events, name .. ":" .. (event.payload.session_id or "") .. ":" .. (event.payload.role or ""))
  end)
end
lc.register_command("events", "events", function()
  return table.concat(events, "\n")
end)
`)

	response, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "lifecycle", ""))
	require.NoError(t, err)

	output, err := manager.ExecuteCommand(context.Background(), "events", "")
	require.NoError(t, err)
	assert.Contains(t, output, "input::")
	assert.Contains(t, output, "prompt_prepare::")
	assert.Contains(t, output, "session_start:"+response.SessionID+":")
	assert.Contains(t, output, "before_agent_start:"+response.SessionID+":")
	assert.Contains(t, output, "agent_start:"+response.SessionID+":")
	assert.Contains(t, output, "turn_start:"+response.SessionID+":")
	assert.Contains(t, output, "message_append:"+response.SessionID+":user")
	assert.Contains(t, output, "message_append:"+response.SessionID+":assistant")
	assert.Contains(t, output, "turn_end:"+response.SessionID+":")
	assert.Contains(t, output, "agent_end:"+response.SessionID+":")
}

func TestRuntime_PromptLifecyclePublishesReactiveEventStream(t *testing.T) {
	t.Parallel()

	runtime, _, _ := newTestRuntimeWithManager(t, testCompletionClient{})
	events := []string{}
	subscription := runtimeEventStream(t, runtime).Channel("turn_start").Subscribe(ro.NewObserver(
		func(envelope event.Envelope) {
			events = append(events, envelope.Channel)
		},
		func(error) {},
		func() {},
	))
	defer subscription.Unsubscribe()

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "stream lifecycle", ""))
	require.NoError(t, err)

	assert.Equal(t, []string{"turn_start"}, events)
}

func TestRuntime_PromptEmitsSessionLoadForExistingSession(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompletionClient{})
	ctx := context.Background()
	firstResponse, err := runtime.Prompt(ctx, newRuntimePromptRequest(testRuntimeCWD, "first", ""))
	require.NoError(t, err)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
local seen = ""
lc.on("session_load", function(event)
  seen = event.payload.session_id
end)
lc.register_command("loaded", "loaded", function()
  return seen
end)
`)

	request := newRuntimePromptRequest(testRuntimeCWD, "second", "")
	request.SessionID = firstResponse.SessionID
	_, err = runtime.Prompt(ctx, request)
	require.NoError(t, err)

	output, err := manager.ExecuteCommand(ctx, "loaded", "")
	require.NoError(t, err)
	assert.Equal(t, firstResponse.SessionID, output)
}

func TestRuntime_PromptEmitsTurnEndOnPromptError(t *testing.T) {
	t.Parallel()

	client := &retryCompletionClient{
		err:               errors.New("bad request"),
		response:          "unused",
		attempts:          0,
		failuresRemaining: 1,
	}
	runtime, _, manager := newTestRuntimeWithManager(t, client)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
local seen = ""
lc.on("turn_end", function(event)
  seen = event.payload.error
end)
lc.register_command("turn_error", "turn_error", function()
  return seen
end)
`)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "fail", ""))
	require.Error(t, err)

	output, commandErr := manager.ExecuteCommand(context.Background(), "turn_error", "")
	require.NoError(t, commandErr)
	assert.Contains(t, output, "bad request")
}

func TestRuntime_PromptEmitsSideEffectMessageAppendEvents(t *testing.T) {
	t.Parallel()

	client := staticCompletionClient{
		result: &assistant.CompletionResult{
			Text:     "done",
			Thinking: []string{"reasoning"},
			ToolEvents: []assistant.ToolEvent{
				{
					Name:          "read",
					ArgumentsJSON: `{"path":"README.md"}`,
					DetailsJSON:   "",
					Result:        "contents",
					Error:         "",
				},
			},
			Usage: model.EmptyTokenUsage(),
		},
		err: nil,
	}
	runtime, _, manager := newTestRuntimeWithManager(t, client)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
local roles = {}
lc.on("message_append", function(event)
  table.insert(roles, event.payload.role)
end)
lc.register_command("roles", "roles", function()
  return table.concat(roles, ",")
end)
`)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "side effects", ""))
	require.NoError(t, err)

	output, err := manager.ExecuteCommand(context.Background(), "roles", "")
	require.NoError(t, err)
	assert.Equal(t, "user,thinking,toolResult,assistant", output)
}

func TestRuntime_PromptLifecycleIgnoresHandlerErrors(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompletionClient{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("turn_start", function()
  error("boom")
end)
`)

	response, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "still works", ""))

	require.NoError(t, err)
	assert.Contains(t, response.Text, "still works")
}

func runtimeEventStream(t *testing.T, runtime *assistant.Runtime) *event.Bus {
	t.Helper()

	bus := runtime.EventBus()
	require.NotNil(t, bus)

	return bus
}

type staticCompletionClient struct {
	result *assistant.CompletionResult
	err    error
}

func (client staticCompletionClient) Complete(
	_ context.Context,
	_ *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if client.err != nil {
		return nil, client.err
	}

	return client.result, nil
}
