package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestCompleteOpenAIChatExecutesNativeToolCalls(t *testing.T) {
	t.Parallel()

	requests, result, roundUsage := completeOpenAIChatWithResponses(
		t,
		openAIChatReadToolResponse(),
		openAIChatTextStream("done"),
	)

	require.Equal(t, "done", result.Text)
	require.Len(t, result.ToolEvents, 1)
	assert.Equal(t, expectedReadToolName, result.ToolEvents[0].Name)
	assert.Contains(t, result.ToolEvents[0].Result, "librecode")
	require.Len(t, requests, 2)
	assert.Equal(t, []llm.Usage{
		reportedRoundUsage(4, 2),
		reportedRoundUsage(6, 3),
	}, roundUsage)
	assert.Equal(t, 10, result.Usage.InputTokens)
	assert.Equal(t, 5, result.Usage.OutputTokens)

	tools, ok := requests[0]["tools"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, tools)

	messages, ok := requests[1]["messages"].([]any)
	require.True(t, ok)
	assert.True(t, containsRoleMessage(messages, jsonToolRole))
}

func TestCompleteOpenAIResponsesAppliesProviderHookEachIteration(t *testing.T) {
	t.Parallel()

	workspace := testToolWorkspace(t)
	captures := make(chan providerResponseHookCapture, 2)

	var (
		hookIterations []int
		roundUsage     []llm.Usage
	)

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++

		captureProviderHookRequest(t, writer, request, captures)
		writeProviderHookIterationResponse(t, writer, requestCount)
	}))
	t.Cleanup(server.Close)

	request := testCompletionRequestAuth("sk-test")
	setTestRequestCWD(request, workspace)
	setTestRequestProvider(request, testOpenAIProvider)
	setTestRequestAPI(request, apiOpenAIResponses)
	setTestRequestBaseURL(request, server.URL)
	installTestToolExecutor(request)
	request.OnProviderResponse = func(_ context.Context, usage llm.Usage) {
		roundUsage = append(roundUsage, usage)
	}
	request.OnProviderRequest = func(
		_ context.Context,
		input *llm.HookInput,
	) (llm.HookOutput, error) {
		iteration := len(hookIterations) + 1
		hookIterations = append(hookIterations, iteration)
		payload := cloneAnyMap(input.Payload)
		payload["iteration"] = iteration
		headers := cloneStringMap(input.Headers)
		headers["X-Iteration"] = strconv.Itoa(iteration)

		return llm.HookOutput{Payload: payload, Headers: headers}, nil
	}

	client := &HTTPCompletionClient{client: server.Client()}
	result, err := client.completeOpenAIResponses(context.Background(), request)
	require.NoError(t, err)

	response := providerResponseView(result)
	assert.Equal(t, "done", response.Text)

	first := <-captures
	second := <-captures

	require.NoError(t, first.Err)
	require.NoError(t, second.Err)
	assert.Equal(t, []int{1, 2}, hookIterations)
	assert.Equal(t, []llm.Usage{
		reportedRoundUsage(8, 1),
		reportedRoundUsage(9, 2),
	}, roundUsage)
	assert.Equal(t, 17, result.Usage.InputTokens)
	assert.Equal(t, 3, result.Usage.OutputTokens)
	assert.Equal(t, "1", first.Header)
	assert.Equal(t, "2", second.Header)
	assert.InDelta(t, 1, first.Body["iteration"], 0)
	assert.InDelta(t, 2, second.Body["iteration"], 0)
}

func captureProviderHookRequest(
	t *testing.T,
	writer http.ResponseWriter,
	request *http.Request,
	captures chan<- providerResponseHookCapture,
) {
	t.Helper()

	capture := providerResponseHookCapture{
		Err:    nil,
		Body:   map[string]any{},
		Header: request.Header.Get("X-Iteration"),
	}
	if err := json.NewDecoder(request.Body).Decode(&capture.Body); err != nil {
		capture.Err = err
	}

	captures <- capture

	writer.Header().Set("Content-Type", "application/json")
}

func writeProviderHookIterationResponse(t *testing.T, writer http.ResponseWriter, requestCount int) {
	t.Helper()

	writer.Header().Set("Content-Type", "text/event-stream")

	if requestCount != 1 {
		writeTestProviderResponse(
			t,
			writer,
			openAIResponseCompletedStream(`{
				"output_text":"done",
				"usage":{"input_tokens":9,"output_tokens":2}
			}`),
		)

		return
	}

	arguments, err := json.Marshal(map[string]string{jsonPathKey: testToolPath})
	require.NoError(t, err)

	response := map[string]any{}
	require.NoError(t, json.Unmarshal(
		[]byte(responseFunctionCallJSON("call_1", jsonReadToolName, string(arguments))),
		&response,
	))
	response[jsonUsageKey] = map[string]any{jsonInputTokensKey: 8, jsonOutputTokensKey: 1}
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	writeTestProviderResponse(t, writer, openAIResponseCompletedStream(string(encoded)))
}

type providerResponseHookCapture struct {
	Err    error
	Body   map[string]any
	Header string
}

func completeOpenAIChatWithResponses(
	t *testing.T,
	firstResponse string,
	secondResponse string,
) ([]map[string]any, providerResponse, []llm.Usage) {
	t.Helper()

	var (
		requests   []map[string]any
		roundUsage []llm.Usage
	)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		requests = append(requests, payload)

		writer.Header().Set("Content-Type", "text/event-stream")

		if len(requests) == 1 {
			writeTestProviderResponse(t, writer, firstResponse)

			return
		}

		writeTestProviderResponse(t, writer, secondResponse)
	}))
	t.Cleanup(server.Close)

	request := testCompletionRequestAuth("sk-test")
	setTestRequestCWD(request, testToolWorkspace(t))
	setTestRequestBaseURL(request, server.URL)
	installTestToolExecutor(request)
	request.OnProviderResponse = func(_ context.Context, usage llm.Usage) {
		roundUsage = append(roundUsage, usage)
	}

	client := &HTTPCompletionClient{client: server.Client()}
	result, err := client.completeOpenAIChat(context.Background(), request)
	require.NoError(t, err)

	return requests, providerResponseView(result), roundUsage
}

func openAIChatReadToolResponse() string {
	arguments, err := json.Marshal(map[string]string{jsonPathKey: "README.md"})
	if err != nil {
		panic(err)
	}

	return openAIChatToolCallStreamWithUsage(
		"call_1",
		jsonReadToolName,
		string(arguments),
		map[string]any{jsonPromptTokensKey: 4, jsonCompletionTokensKey: 2},
	)
}

func openAIChatToolCallStreamWithUsage(callID, name, arguments string, usage map[string]any) string {
	choice := map[string]any{
		anthropicDeltaKey: map[string]any{jsonToolCallsKey: []any{map[string]any{
			jsonIndexKey: 0,
			"id":         callID,
			"type":       functionToolType,
			jsonFunctionKey: map[string]any{
				jsonToolNameKey:  name,
				jsonArgumentsKey: arguments,
			},
		}}},
		jsonFinishReasonKey: jsonToolCallsKey,
	}
	if len(usage) > 0 {
		choice[jsonUsageKey] = usage
	}

	return openAIChatStream(
		openAIChatChunk(map[string]any{jsonChoicesKey: []any{choice}}),
		openAIChatDoneLine,
	)
}

func openAIChatTextStream(text string) string {
	return openAIChatStream(
		openAIChatDelta(
			map[string]any{jsonContentKey: text},
			"stop",
			map[string]any{jsonPromptTokensKey: 6, jsonCompletionTokensKey: 3},
		),
		openAIChatDoneLine,
	)
}

func containsRoleMessage(messages []any, role string) bool {
	for _, message := range messages {
		object, matched := message.(map[string]any)
		if matched && object[jsonRoleKey] == role {
			return true
		}
	}

	return false
}

func writeTestProviderResponse(t *testing.T, writer http.ResponseWriter, response string) {
	t.Helper()

	_, err := writer.Write([]byte(response))
	require.NoError(t, err)
}

func testToolWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	readmePath := filepath.Join(workspace, "README.md")
	require.NoError(t, os.WriteFile(readmePath, []byte("# librecode\n"), 0o600))

	return workspace
}
