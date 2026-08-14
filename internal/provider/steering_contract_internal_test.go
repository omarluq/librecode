package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

type steeringProviderFamily struct {
	name string
	api  string
}

func TestProviderSteeringContract(t *testing.T) {
	t.Parallel()

	families := []steeringProviderFamily{
		{name: "anthropic", api: apiAnthropicMessages},
		{name: "openai_chat", api: apiOpenAICompletions},
		{name: "openai_responses", api: apiOpenAIResponses},
		{name: "codex_responses", api: apiOpenAICodexResponses},
	}
	tests := []struct {
		run  func(*testing.T, steeringProviderFamily)
		name string
	}{
		{name: "no steering leaves request unchanged", run: testSteeringNoop},
		{name: "text continuation fifo multipart usage and hooks", run: testSteeringTextContinuation},
		{name: "complete tool batch precedes steering", run: testSteeringToolBatch},
		{name: "invalid checkpoint image stops before continuation", run: testSteeringInvalidImage},
		{name: "provider failure and cancellation do not checkpoint", run: testSteeringFailure},
		{name: "retry replays stable payload before steering", run: testSteeringRetryStability},
	}

	for _, family := range families {
		for _, testCase := range tests {
			t.Run(family.name+"/"+testCase.name, func(t *testing.T) {
				t.Parallel()
				testCase.run(t, family)
			})
		}
	}
}

func TestOpenAIChatBlankSteeringContinuationOmitsAssistantMessage(t *testing.T) {
	t.Parallel()

	family := steeringProviderFamily{name: "openai_chat", api: apiOpenAICompletions}
	checkpointCalls := 0
	requests, _, _, _ := runSteeringProvider(
		t,
		family,
		[]string{family.textResponse("   ", 1, 0), family.textResponse("done", 1, 1)},
		func(context.Context, *llm.CompletedRound) ([]llm.Message, error) {
			checkpointCalls++
			if checkpointCalls == 1 {
				return []llm.Message{llm.TextMessage(llm.RoleUser, "steer")}, nil
			}

			return nil, nil
		},
	)

	require.Len(t, requests, 2)
	conversation := family.requestConversation(requests[1])
	require.Len(t, conversation, 1)
	message, ok := conversation[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, []any{jsonUserRole}, message[jsonRoleKey])
}

func TestOpenAIChatBlankToolCallMessageIsPreserved(t *testing.T) {
	t.Parallel()

	state := &openAIChatLoopState{result: nil, endpoint: "", messages: nil}
	result := &providerResult{Termination: llm.NewTerminationMetadata("", "", ""),
		FinishReason: "", Text: "", OutputItems: nil, Thinking: nil,
		ToolCalls: []ToolCall{{
			Metadata: nil, ArgumentsJSON: "", ID: testCallID, Name: jsonReadToolName, Arguments: tool.EmptyArguments(),
		}}, Usage: llm.EmptyUsage(),
	}
	events := []ToolEvent{{
		ArgumentsJSON: "", DetailsJSON: "", Result: "", Error: "",
		Name: jsonReadToolName, IsError: false,
	}}

	err := appendOpenAIChatToolConversation(state, result, events)

	require.NoError(t, err)
	require.NotEmpty(t, state.messages)
	role, ok := state.messages[0][jsonRoleKey].(string)
	require.True(t, ok)
	assert.Contains(t, []string{jsonAssistantRole}, role)
	assert.NotEmpty(t, state.messages[0][jsonToolCallsKey])
}

func testSteeringNoop(t *testing.T, family steeringProviderFamily) {
	t.Helper()

	responses := []string{family.textResponse("done", 2, 1)}
	withoutCallback, _, _, _ := runSteeringProvider(t, family, responses, nil)
	withEmptyCallback, _, checkpointCalls, _ := runSteeringProvider(
		t,
		family,
		responses,
		func(context.Context, *llm.CompletedRound) ([]llm.Message, error) { return nil, nil },
	)

	require.Len(t, withoutCallback, 1)
	require.Len(t, withEmptyCallback, 1)
	assert.Equal(t, withoutCallback[0], withEmptyCallback[0])
	assert.Equal(t, 1, checkpointCalls)
}

func testSteeringTextContinuation(t *testing.T, family steeringProviderFamily) {
	t.Helper()

	image := llm.Part{
		Metadata: nil, ToolCall: nil, ToolResult: nil,
		Type: llm.PartImage, Text: "", Data: "aQ==", MIMEType: testImageMIME,
	}
	checkpointCalls := 0
	requests, result, _, hookCalls := runSteeringProvider(
		t,
		family,
		[]string{family.textResponse("first", 2, 1), family.textResponse("second", 3, 4)},
		func(_ context.Context, round *llm.CompletedRound) ([]llm.Message, error) {
			checkpointCalls++
			if checkpointCalls != 1 {
				return nil, nil
			}

			assert.Equal(t, reportedRoundUsage(2, 1), round.Usage)

			return []llm.Message{
				llm.TextMessage(llm.RoleUser, "first steer"),
				{Metadata: nil, Role: llm.RoleUser, Content: []llm.Part{llm.TextPart("second steer"), image}},
			}, nil
		},
	)

	assert.Equal(t, "second", responseText(result))
	assert.Equal(t, 5, result.Usage.InputTokens)
	assert.Equal(t, 5, result.Usage.OutputTokens)
	assert.Equal(t, 2, checkpointCalls)
	assert.Equal(t, len(requests), hookCalls)
	require.Len(t, requests, 2)
	family.assertSteeringOrder(t, requests[1], []string{"first steer", "second steer"}, true)
}

func testSteeringToolBatch(t *testing.T, family steeringProviderFamily) {
	t.Helper()

	checkpointCalls := 0
	requests, result, _, _ := runSteeringProvider(
		t,
		family,
		[]string{family.toolBatchResponse(), family.textResponse("done", 5, 2)},
		func(_ context.Context, round *llm.CompletedRound) ([]llm.Message, error) {
			checkpointCalls++
			if checkpointCalls != 1 {
				return nil, nil
			}

			require.Len(t, round.ToolResults, 2)

			return []llm.Message{llm.TextMessage(llm.RoleUser, "after tools")}, nil
		},
	)

	assert.Equal(t, "done", responseText(result))
	require.Len(t, responseToolEvents(result), 2)
	require.Len(t, requests, 2)
	family.assertToolResultsBeforeSteering(t, requests[1], "after tools")
}

func testSteeringInvalidImage(t *testing.T, family steeringProviderFamily) {
	t.Helper()

	badImage := llm.Part{
		Metadata: nil, ToolCall: nil, ToolResult: nil,
		Type: llm.PartImage, Text: "", Data: "not-base64", MIMEType: testImageMIME,
	}
	requests, _, _, _ := runSteeringProviderExpectError(
		t,
		family,
		[]string{family.textResponse("first", 1, 1)},
		func(context.Context, *llm.CompletedRound) ([]llm.Message, error) {
			return []llm.Message{{Metadata: nil, Role: llm.RoleUser, Content: []llm.Part{badImage}}}, nil
		},
	)
	assert.Len(t, requests, 1)
}

func testSteeringFailure(t *testing.T, family steeringProviderFamily) {
	t.Helper()

	var calls atomic.Int32

	callback := func(context.Context, *llm.CompletedRound) ([]llm.Message, error) {
		calls.Add(1)

		return nil, nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	request := steeringCompletionRequest(t, family, server.URL)
	request.OnRoundCheckpoint = callback
	_, err := (&HTTPCompletionClient{client: server.Client()}).Complete(t.Context(), request)
	require.Error(t, err)
	assert.Zero(t, calls.Load())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	request = steeringCompletionRequest(t, family, server.URL)
	request.OnRoundCheckpoint = callback
	_, err = (&HTTPCompletionClient{client: server.Client()}).Complete(ctx, request)
	require.Error(t, err)
	assert.Zero(t, calls.Load())
}

func testSteeringRetryStability(t *testing.T, family steeringProviderFamily) {
	t.Helper()

	var payloads []map[string]any

	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)

			return
		}

		payloads = append(payloads, payload)

		attempt++

		if attempt == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		writer.Header().Set("Content-Type", "text/event-stream")

		body := family.textResponse("first", 1, 1)
		if attempt == 3 {
			body = family.textResponse("second", 1, 1)
		}

		writeTestProviderResponse(t, writer, body)
	}))
	t.Cleanup(server.Close)

	checkpointCalls := 0
	request := steeringCompletionRequest(t, family, server.URL)
	request.OnRoundCheckpoint = func(context.Context, *llm.CompletedRound) ([]llm.Message, error) {
		checkpointCalls++
		if checkpointCalls != 1 {
			return nil, nil
		}

		return []llm.Message{llm.TextMessage(llm.RoleUser, "steer")}, nil
	}

	client := &HTTPCompletionClient{client: server.Client()}
	_, err := client.Complete(t.Context(), request)
	require.Error(t, err)
	assert.Zero(t, checkpointCalls)

	result, err := client.Complete(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, "second", responseText(result))
	require.Len(t, payloads, 3)
	assert.Equal(t, payloads[0], payloads[1])
	family.assertSteeringOrder(t, payloads[2], []string{"steer"}, false)
}

func runSteeringProvider(
	t *testing.T,
	family steeringProviderFamily,
	responses []string, checkpoint llm.RoundCheckpoint,
) (requests []map[string]any, result *llm.Response, checkpointCalls, hookCalls int) {
	t.Helper()

	requests, result, checkpointCalls, hookCalls, err := runSteeringProviderResult(t, family, responses, checkpoint)
	require.NoError(t, err)

	return requests, result, checkpointCalls, hookCalls
}

func runSteeringProviderExpectError(
	t *testing.T,
	family steeringProviderFamily,
	responses []string, checkpoint llm.RoundCheckpoint,
) (requests []map[string]any, result *llm.Response, checkpointCalls, hookCalls int) {
	t.Helper()

	requests, result, checkpointCalls, hookCalls, err := runSteeringProviderResult(t, family, responses, checkpoint)
	require.Error(t, err)

	return requests, result, checkpointCalls, hookCalls
}

func runSteeringProviderResult(
	t *testing.T,
	family steeringProviderFamily,
	responses []string, checkpoint llm.RoundCheckpoint,
) (requests []map[string]any, result *llm.Response, checkpointCalls, hookCalls int, resultErr error) {
	t.Helper()

	requests = []map[string]any{}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)

			return
		}

		requests = append(requests, payload)
		if len(requests) > len(responses) {
			http.Error(writer, "unexpected request", http.StatusInternalServerError)

			return
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		writeTestProviderResponse(t, writer, responses[len(requests)-1])
	}))
	t.Cleanup(server.Close)

	checkpointCalls = 0
	hookCalls = 0

	request := steeringCompletionRequest(t, family, server.URL)
	if checkpoint != nil {
		request.OnRoundCheckpoint = func(ctx context.Context, round *llm.CompletedRound) ([]llm.Message, error) {
			checkpointCalls++

			return checkpoint(ctx, round)
		}
	}

	request.OnProviderRequest = func(_ context.Context, input *llm.HookInput) (llm.HookOutput, error) {
		hookCalls++

		return llm.HookOutput{Payload: input.Payload, Headers: input.Headers}, nil
	}

	result, resultErr = (&HTTPCompletionClient{client: server.Client()}).Complete(t.Context(), request)

	return requests, result, checkpointCalls, hookCalls, resultErr
}

func steeringCompletionRequest(t *testing.T, family steeringProviderFamily, baseURL string) *CompletionRequest {
	t.Helper()

	request := testCompletionRequestAuth("sk-test")
	setTestRequestAPI(request, family.api)
	setTestRequestBaseURL(request, baseURL)
	setTestRequestCWD(request, testToolWorkspace(t))
	installTestToolExecutor(request)

	return request
}

func (family steeringProviderFamily) textResponse(text string, inputTokens, outputTokens int) string {
	switch family.api {
	case apiAnthropicMessages:
		return anthropicResponseStream(`{"stop_reason":"end_turn","usage":{"input_tokens":` +
			jsonString(inputTokens) + `,"output_tokens":` + jsonString(outputTokens) +
			`},"content":[{"type":"text","text":` + jsonString(text) + `}]}`)
	case apiOpenAICompletions:
		return openAIChatStream(openAIChatDelta(
			map[string]any{jsonContentKey: text},
			openAIStopReason,
			map[string]any{jsonPromptTokensKey: inputTokens, jsonCompletionTokensKey: outputTokens},
		), openAIChatDoneLine)
	default:
		return openAIResponseCompletedStream(`{"status":"completed","output_text":` + jsonString(text) +
			`,"usage":{"input_tokens":` + jsonString(inputTokens) + `,"output_tokens":` +
			jsonString(outputTokens) + `}}`)
	}
}

func (family steeringProviderFamily) toolBatchResponse() string {
	arguments := map[string]any{jsonPathKey: "README.md"}

	switch family.api {
	case apiAnthropicMessages:
		return anthropicResponseStream(`{"stop_reason":"tool_use","usage":{"input_tokens":2,"output_tokens":1},` +
			`"content":[{"type":"tool_use","id":"call_1","name":"read","input":` + jsonString(arguments) + `},` +
			`{"type":"tool_use","id":"call_2","name":"read","input":` + jsonString(arguments) + `}]}`)
	case apiOpenAICompletions:
		calls := []any{}
		for index, id := range []string{testCallID, "call_2"} {
			calls = append(calls, map[string]any{
				jsonIndexKey: index, "id": id, jsonTypeKey: functionToolType,
				jsonFunctionKey: map[string]any{
					jsonToolNameKey: jsonReadToolName, jsonArgumentsKey: jsonString(arguments),
				},
			})
		}

		return openAIChatStream(openAIChatChunk(map[string]any{jsonChoicesKey: []any{map[string]any{
			anthropicDeltaKey: map[string]any{jsonToolCallsKey: calls}, jsonFinishReasonKey: openAIToolCallsReason,
		}}, jsonUsageKey: map[string]any{jsonPromptTokensKey: 2, jsonCompletionTokensKey: 1}}), openAIChatDoneLine)
	default:
		output := []any{}
		for _, id := range []string{testCallID, "call_2"} {
			output = append(output, map[string]any{
				jsonTypeKey: functionCallType, jsonCallIDKey: id, jsonToolNameKey: jsonReadToolName,
				jsonArgumentsKey: jsonString(arguments),
			})
		}

		return openAIResponseCompletedStream(jsonString(map[string]any{
			"status": statusCompleted, jsonOutputKey: output,
			jsonUsageKey: map[string]any{jsonInputTokensKey: 2, jsonOutputTokensKey: 1},
		}))
	}
}

func (family steeringProviderFamily) requestConversation(payload map[string]any) []any {
	key := jsonInputKey
	if family.api == apiAnthropicMessages || family.api == apiOpenAICompletions {
		key = jsonMessagesKey
	}

	conversation, matched := payload[key].([]any)
	if !matched {
		return nil
	}

	return conversation
}

func (family steeringProviderFamily) assertSteeringOrder(
	t *testing.T,
	payload map[string]any,
	texts []string,
	wantImage bool,
) {
	t.Helper()

	conversation := family.requestConversation(payload)
	encoded := jsonString(conversation)

	last := -1
	for _, text := range texts {
		index := indexAfter(encoded, text, last+1)
		assert.Greater(t, index, last, encoded)
		last = index
	}

	if wantImage {
		assert.Contains(t, encoded, "aQ==")
		assert.Contains(t, encoded, "image/png")
	}
}

func (family steeringProviderFamily) assertToolResultsBeforeSteering(
	t *testing.T,
	payload map[string]any,
	steering string,
) {
	t.Helper()

	encoded := jsonString(family.requestConversation(payload))

	steeringIndex := indexAfter(encoded, steering, 0)
	require.Positive(t, steeringIndex, encoded)

	marker := functionCallOutputType

	switch family.api {
	case apiOpenAICompletions:
		marker = `"role":"tool"`
	case apiAnthropicMessages:
		marker = anthropicToolResultType
	}

	first := indexAfter(encoded, marker, 0)
	second := indexAfter(encoded, marker, first+1)
	assert.GreaterOrEqual(t, first, 0, encoded)
	assert.Greater(t, second, first, encoded)
	assert.Less(t, second, steeringIndex, encoded)
}

func indexAfter(value, substring string, start int) int {
	if start < 0 || start >= len(value) {
		return -1
	}

	index := strings.Index(value[start:], substring)
	if index < 0 {
		return -1
	}

	return start + index
}
