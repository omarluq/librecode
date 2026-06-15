package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestCompleteOpenAIChatExecutesNativeToolCalls(t *testing.T) {
	t.Parallel()

	requests, result := completeOpenAIChatWithResponses(
		t,
		openAIChatReadToolResponse(),
		openAIChatTextStream("done"),
	)

	require.Equal(t, "done", result.Text)
	require.Len(t, result.ToolEvents, 1)
	assert.Equal(t, expectedReadToolName, result.ToolEvents[0].Name)
	assert.Contains(t, result.ToolEvents[0].Result, "librecode")
	require.Len(t, requests, 2)
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

	var hookIterations []int

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
		writeTestProviderResponse(t, writer, openAIResponseCompletedStream(`{"output_text":"done"}`))

		return
	}

	arguments, err := json.Marshal(map[string]string{jsonPathKey: testToolPath})
	require.NoError(t, err)
	writeTestProviderResponse(
		t,
		writer,
		openAIResponseCompletedStream(responseFunctionCallJSON("call_1", jsonReadToolName, string(arguments))),
	)
}

type providerResponseHookCapture struct {
	Err    error
	Body   map[string]any
	Header string
}

func TestCompleteAnthropicExecutesTextToolUseFallback(t *testing.T) {
	t.Parallel()

	var requests []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		requests = append(requests, payload)

		writer.Header().Set("Content-Type", "text/event-stream")

		if len(requests) == 1 {
			writeTestProviderResponse(t, writer, anthropicTextReadToolResponse())

			return
		}

		writeTestProviderResponse(t, writer, anthropicTextStream("done", "end_turn"))
	}))
	defer server.Close()

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	setTestRequestCWD(request, testToolWorkspace(t))
	setTestRequestBaseURL(request, server.URL)
	installTestToolExecutor(request)

	result, err := (&HTTPCompletionClient{client: server.Client()}).completeAnthropic(context.Background(), request)
	require.NoError(t, err)

	response := providerResponseView(result)
	require.Equal(t, "done", response.Text)
	require.Len(t, response.ToolEvents, 1)
	assert.Equal(t, expectedReadToolName, response.ToolEvents[0].Name)
	assert.Contains(t, response.ToolEvents[0].Result, "librecode")
	require.Len(t, requests, 2)
	messages, ok := requests[1]["messages"].([]any)
	require.True(t, ok)
	assert.True(t, containsAssistantTextToolUsePrompt(messages))
	assert.True(t, containsUserToolResultPrompt(messages))
}

func completeOpenAIChatWithResponses(
	t *testing.T,
	firstResponse string,
	secondResponse string,
) ([]map[string]any, providerResponse) {
	t.Helper()

	var requests []map[string]any

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

	result, err := (&HTTPCompletionClient{client: server.Client()}).completeOpenAIChat(context.Background(), request)
	require.NoError(t, err)

	return requests, providerResponseView(result)
}

func openAIChatReadToolResponse() string {
	arguments, err := json.Marshal(map[string]string{jsonPathKey: "README.md"})
	if err != nil {
		panic(err)
	}

	return openAIChatToolCallStream("call_1", jsonReadToolName, string(arguments))
}

func openAIChatToolCallStream(callID, name, arguments string) string {
	return openAIChatStream(
		openAIChatChunk(map[string]any{jsonChoicesKey: []any{map[string]any{
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
		}}}),
		openAIChatDoneLine,
	)
}

func openAIChatTextStream(text string) string {
	return openAIChatStream(
		openAIChatChunk(map[string]any{jsonChoicesKey: []any{map[string]any{
			anthropicDeltaKey:   map[string]any{jsonContentKey: text},
			jsonFinishReasonKey: "stop",
		}}}),
		openAIChatDoneLine,
	)
}

func anthropicTextReadToolResponse() string {
	return anthropicTextStream(anthropicTextReadToolMarkup(), "end_turn")
}

func anthropicTextStream(text, stopReason string) string {
	lines := make([]string, 0, 9)
	lines = append(lines,
		anthropicEventContentBlockDelta,
		"data: "+anthropicContentDeltaJSON(0, anthropicTextDelta, jsonTextKey, text),
		"",
	)
	lines = append(lines, anthropicMessageDeltaLines(stopReason, nil)...)

	return strings.Join(lines, "\n")
}

func anthropicTextReadToolMarkup() string {
	return "<tool_use><tool_name>Read</tool_name><file_path>README.md</file_path></tool_use>"
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

func containsAssistantTextToolUsePrompt(messages []any) bool {
	for _, message := range messages {
		object, matched := message.(map[string]any)
		if !matched || object[jsonRoleKey] != jsonAssistantRole {
			continue
		}

		content, ok := object[jsonContentKey].(string)
		if ok && strings.Contains(content, "<tool_use>") {
			return true
		}
	}

	return false
}

func containsUserToolResultPrompt(messages []any) bool {
	for _, message := range messages {
		object, matched := message.(map[string]any)
		if !matched || object[jsonRoleKey] != jsonUserRole {
			continue
		}

		content, ok := object[jsonContentKey].(string)
		if ok && strings.HasPrefix(content, "Tool result for read") {
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

func TestCompleteOpenAIChatExecutesTextToolUseFallback(t *testing.T) {
	t.Parallel()

	requests, result := completeOpenAIChatWithResponses(
		t,
		openAIChatTextStream(anthropicTextReadToolMarkup()),
		openAIChatTextStream("done"),
	)

	require.Equal(t, "done", result.Text)
	require.Len(t, result.ToolEvents, 1)
	assert.Equal(t, expectedReadToolName, result.ToolEvents[0].Name)
	require.Len(t, requests, 2)
	messages, ok := requests[1]["messages"].([]any)
	require.True(t, ok)
	assert.True(t, containsAssistantTextToolUsePrompt(messages))
	assert.True(t, containsUserToolResultPrompt(messages))
}
