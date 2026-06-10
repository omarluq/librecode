//nolint:testpackage // Tests exercise provider-specific unexported tool-loop helpers.
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
		`{"choices":[{"message":{"content":"done"}}]}`,
	)

	require.Equal(t, "done", result.Text)
	require.Len(t, result.ToolEvents, 1)
	assert.Equal(t, jsonReadToolName, result.ToolEvents[0].Name)
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
	var requestCount int
	var hookIterations []int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		capture := providerResponseHookCapture{
			Err:    nil,
			Body:   map[string]any{},
			Header: request.Header.Get("X-Iteration"),
		}
		if err := json.NewDecoder(request.Body).Decode(&capture.Body); err != nil {
			capture.Err = err
			captures <- capture
			return
		}
		requestCount++
		captures <- capture
		writer.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			arguments, err := json.Marshal(map[string]string{jsonPathKey: testToolPath})
			require.NoError(t, err)
			writeTestProviderResponse(
				t,
				writer,
				`{"output":[{"type":"function_call","call_id":"call_1","name":"read","arguments":`+
					strconv.Quote(string(arguments))+
					`}]}`,
			)
			return
		}
		writeTestProviderResponse(t, writer, `{"output_text":"done"}`)
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

	result, err := NewHTTPCompletionClient().completeOpenAIResponses(context.Background(), request)
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
	assert.Equal(t, float64(1), first.Body["iteration"])
	assert.Equal(t, float64(2), second.Body["iteration"])
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
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		requests = append(requests, payload)
		writer.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			writeTestProviderResponse(t, writer, anthropicTextReadToolResponse())
			return
		}
		writeTestProviderResponse(t, writer, `{"content":[{"type":"text","text":"done"}]}`)
	}))
	defer server.Close()

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	setTestRequestCWD(request, testToolWorkspace(t))
	setTestRequestBaseURL(request, server.URL)
	installTestToolExecutor(request)

	result, err := NewHTTPCompletionClient().completeAnthropic(context.Background(), request)
	require.NoError(t, err)
	response := providerResponseView(result)
	require.Equal(t, "done", response.Text)
	require.Len(t, response.ToolEvents, 1)
	assert.Equal(t, jsonReadToolName, response.ToolEvents[0].Name)
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
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		requests = append(requests, payload)
		writer.Header().Set("Content-Type", "application/json")
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

	result, err := NewHTTPCompletionClient().completeOpenAIChat(context.Background(), request)
	require.NoError(t, err)

	return requests, providerResponseView(result)
}

func openAIChatReadToolResponse() string {
	arguments, err := json.Marshal(map[string]string{jsonPathKey: "README.md"})
	if err != nil {
		panic(err)
	}
	encodedArguments, err := json.Marshal(string(arguments))
	if err != nil {
		panic(err)
	}

	return `{
		"choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{
			"name":"read",
			"arguments":` + string(encodedArguments) + `
		}}]}}]
	}`
}

func anthropicTextReadToolResponse() string {
	return `{ "content":[{"type":"text","text":"` + anthropicTextReadToolMarkup() + `"}] }`
}

func anthropicTextReadToolMarkup() string {
	return "<tool_use><tool_name>Read</tool_name><file_path>README.md</file_path></tool_use>"
}

func containsRoleMessage(messages []any, role string) bool {
	for _, message := range messages {
		object, ok := message.(map[string]any)
		if ok && object[jsonRoleKey] == role {
			return true
		}
	}

	return false
}

func containsAssistantTextToolUsePrompt(messages []any) bool {
	for _, message := range messages {
		object, ok := message.(map[string]any)
		if !ok || object[jsonRoleKey] != jsonAssistantRole {
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
		object, ok := message.(map[string]any)
		if !ok || object[jsonRoleKey] != jsonUserRole {
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

	content, err := json.Marshal(anthropicTextReadToolMarkup())
	require.NoError(t, err)
	requests, result := completeOpenAIChatWithResponses(
		t,
		`{"choices":[{"message":{"content":`+string(content)+`}}]}`,
		`{"choices":[{"message":{"content":"done"}}]}`,
	)

	require.Equal(t, "done", result.Text)
	require.Len(t, result.ToolEvents, 1)
	assert.Equal(t, jsonReadToolName, result.ToolEvents[0].Name)
	require.Len(t, requests, 2)
	messages, ok := requests[1]["messages"].([]any)
	require.True(t, ok)
	assert.True(t, containsAssistantTextToolUsePrompt(messages))
	assert.True(t, containsUserToolResultPrompt(messages))
}
