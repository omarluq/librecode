package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestCompleteAnthropicExecutesToolCalls(t *testing.T) {
	t.Parallel()

	workspace := testToolWorkspace(t)
	requests := make([]map[string]any, 0, 2)
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++

		var payload map[string]any
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		requests = append(requests, payload)

		writer.Header().Set("Content-Type", "text/event-stream")

		if requestCount == 1 {
			writeTestProviderResponse(t, writer, anthropicResponseStream(`{
				"stop_reason":"tool_use",
				"usage":{"input_tokens":1,"output_tokens":2},
				"content":[{"type":"tool_use","id":"tool_1","name":"Read","input":{"path":"README.md"}}]
			}`))

			return
		}

		writeTestProviderResponse(t, writer, anthropicResponseStream(anthropicResponseJSON("end_turn", "done", nil)))
	}))
	t.Cleanup(server.Close)

	request := testCompletionRequestAuth("anthropic-claude", "sk-ant-oat-secret")
	setTestRequestCWD(request, workspace)
	setTestRequestBaseURL(request, server.URL)
	installTestToolExecutor(request)

	result, err := (&HTTPCompletionClient{client: server.Client()}).completeAnthropic(context.Background(), request)

	require.NoError(t, err)

	response := providerResponseView(result)
	assert.Equal(t, "done", response.Text)
	require.Len(t, response.ToolEvents, 1)
	assert.Equal(t, expectedReadToolName, response.ToolEvents[0].Name)
	assert.Contains(t, response.ToolEvents[0].Result, "librecode")
	require.Len(t, requests, 2)
	messages, ok := requests[1][jsonMessagesKey].([]any)
	require.True(t, ok)
	assert.Len(t, messages, 2)
}

func TestAdvanceAnthropicLoopReturnsToolValidationError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeTestProviderResponse(t, writer, anthropicResponseStream(`{
			"stop_reason":"tool_use",
			"content":[{"type":"tool_use","name":"Read","input":{"path":"README.md"}}]
		}`))
	}))
	t.Cleanup(server.Close)

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	setTestRequestBaseURL(request, server.URL)
	state := anthropicLoopState{
		result:   newResponse(),
		endpoint: server.URL + "/v1/messages",
		messages: nil,
	}

	finished, err := (&HTTPCompletionClient{client: server.Client()}).advanceAnthropicLoop(
		context.Background(),
		request,
		&state,
	)

	assert.False(t, finished)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without call_id")
}

func TestAppendAnthropicToolConversationMismatchedEvents(t *testing.T) {
	t.Parallel()

	err := appendAnthropicToolConversation(
		testCompletionRequestAuth("sk-ant-api03-secret"),
		&anthropicLoopState{result: newResponse(), endpoint: "", messages: nil},
		&providerResult{
			FinishReason: llm.FinishReasonUnknown,
			Text:         "",
			OutputItems:  nil,
			Thinking:     nil,
			ToolCalls: []ToolCall{{
				Arguments:     toolArgumentsFromJSON(testToolArgumentsJSON),
				Metadata:      nil,
				ID:            "tool_1",
				Name:          jsonReadToolName,
				ArgumentsJSON: testToolArgumentsJSON,
			}},
			Usage: llm.EmptyUsage(),
		},
		nil,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatched tool calls and results")
}
