package assistant_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samber/ro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/model"
)

const lifecycleRecorderExtension = `
local lc = require("librecode")
local events = {}
local names = {
  "input",
  "prompt_prepare",
  "session_start",
  "before_agent_start",
  "agent_start",
  "turn_start",
  "context_build",
  "message_append",
  "turn_end",
  "agent_end",
}
for _, name in ipairs(names) do
  lc.on(name, function(event)
    local payload = event.payload or {}
    local session = "no-session"
    if payload.session_id and payload.session_id ~= "" then
      session = "session"
    end
    local error_state = ""
    if payload.error and payload.error ~= "" then
      error_state = "error"
    end
    table.insert(events, name .. "|" .. (payload.role or "") .. "|" .. error_state .. "|" .. session)
  end)
end
lc.register_command("events", "events", function()
  return table.concat(events, "\n")
end)
`

type lifecycleOrderTestCase struct {
	expectedEvents *lifecycleExpectedEvents
	name           string
	prompt         string
	expectError    bool
}

type lifecycleExpectedEvents struct {
	items []string
}

func TestRuntime_PromptEmitsOrderedSessionTurnLifecycleEvents(t *testing.T) {
	t.Parallel()

	tests := []lifecycleOrderTestCase{
		{
			name:        "successful prompt",
			prompt:      "lifecycle",
			expectError: false,
			expectedEvents: &lifecycleExpectedEvents{items: []string{
				"input|||no-session",
				"prompt_prepare|||no-session",
				"session_start|||session",
				"message_append|user||session",
				"before_agent_start|||session",
				"agent_start|||session",
				"turn_start|||session",
				"context_build|||session",
				"message_append|assistant||session",
				"turn_end|||session",
				"agent_end|||session",
			}},
		},
		{
			name:        "prompt error",
			prompt:      "fail",
			expectError: true,
			expectedEvents: &lifecycleExpectedEvents{items: []string{
				"input|||no-session",
				"prompt_prepare|||no-session",
				"session_start|||session",
				"message_append|user||session",
				"before_agent_start|||session",
				"agent_start|||session",
				"turn_start|||session",
				"context_build|||session",
				"message_append|custom||session",
				"turn_end||error|session",
				"agent_end||error|session",
			}},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := lifecycleTestClient(testCase.expectError)
			runtime, _, manager := newTestRuntimeWithManager(t, client)
			loadRuntimeExtension(t, manager, lifecycleRecorderExtension)

			_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, testCase.prompt, ""))
			if testCase.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			output, err := manager.ExecuteCommand(context.Background(), "events", "")
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedEvents.items, strings.Split(output, "\n"))
		})
	}
}

func lifecycleTestClient(fails bool) assistant.CompletionClient {
	if !fails {
		return testCompletionClient{}
	}

	return &retryCompletionClient{
		err:               errors.New("bad request"),
		response:          "unused",
		attempts:          0,
		failuresRemaining: 1,
	}
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
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.OnProviderObserve != nil {
		request.OnProviderObserve(ctx, request, request.ProviderAttempt)
	}
	if client.err != nil {
		return nil, client.err
	}

	return client.result, nil
}
