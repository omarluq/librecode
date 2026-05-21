//nolint:testpackage // Tests exercise provider-specific unexported tool-loop helpers.
package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompleteOpenAIChatExecutesNativeToolCalls(t *testing.T) {
	t.Parallel()

	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		requests = append(requests, payload)
		writer.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			writeTestProviderResponse(t, writer, openAIChatReadToolResponse())
			return
		}
		writeTestProviderResponse(t, writer, `{"choices":[{"message":{"content":"done"}}]}`)
	}))
	defer server.Close()

	request := testCompletionRequestAuth("sk-test")
	request.CWD = testToolWorkspace(t)
	request.Model.BaseURL = server.URL

	result, err := NewHTTPCompletionClient().completeOpenAIChat(context.Background(), request)
	require.NoError(t, err)
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
	request.CWD = testToolWorkspace(t)
	request.Model.BaseURL = server.URL

	result, err := NewHTTPCompletionClient().completeAnthropic(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "done", result.Text)
	require.Len(t, result.ToolEvents, 1)
	assert.Equal(t, jsonReadToolName, result.ToolEvents[0].Name)
	assert.Contains(t, result.ToolEvents[0].Result, "librecode")
	require.Len(t, requests, 2)
	messages, ok := requests[1]["messages"].([]any)
	require.True(t, ok)
	assert.True(t, containsAssistantTextToolUsePrompt(messages))
	assert.True(t, containsUserToolResultPrompt(messages))
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

	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		requests = append(requests, payload)
		writer.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			content, err := json.Marshal(anthropicTextReadToolMarkup())
			require.NoError(t, err)
			writeTestProviderResponse(t, writer, `{"choices":[{"message":{"content":`+string(content)+`}}]}`)
			return
		}
		writeTestProviderResponse(t, writer, `{"choices":[{"message":{"content":"done"}}]}`)
	}))
	defer server.Close()

	request := testCompletionRequestAuth("sk-test")
	request.CWD = testToolWorkspace(t)
	request.Model.BaseURL = server.URL

	result, err := NewHTTPCompletionClient().completeOpenAIChat(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "done", result.Text)
	require.Len(t, result.ToolEvents, 1)
	assert.Equal(t, jsonReadToolName, result.ToolEvents[0].Name)
	require.Len(t, requests, 2)
	messages, ok := requests[1]["messages"].([]any)
	require.True(t, ok)
	assert.True(t, containsAssistantTextToolUsePrompt(messages))
	assert.True(t, containsUserToolResultPrompt(messages))
}
