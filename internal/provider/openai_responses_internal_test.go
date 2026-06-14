package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestStatelessResponseOutputItemsFiltersFunctionCalls(t *testing.T) {
	t.Parallel()

	items := []any{
		map[string]any{
			jsonTypeKey:      functionCallType,
			jsonCallIDKey:    testCallID,
			jsonToolNameKey:  jsonReadToolName,
			jsonArgumentsKey: testToolArgumentsJSON,
		},
		map[string]any{jsonTypeKey: jsonMessageType},
		"not an object",
	}

	stateless := statelessResponseOutputItems(items)

	require.Len(t, stateless, 1)
	call, ok := stateless[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, functionCallType, call[jsonTypeKey])
	assert.Equal(t, testCallID, call[jsonCallIDKey])
	assert.Equal(t, "completed", call["status"])
}

func TestParseOpenAIResponseResultExtractsTextThinkingAndToolCalls(t *testing.T) {
	t.Parallel()

	result, err := parseOpenAIResponseResult([]byte(`{
		"output": [
			{"type":"reasoning","summary":[{"text":"thought one"},"thought two"]},
			{"type":"message","content":[{"type":"output_text","text":"hello"}]},
			{"type":"function_call","call_id":"call_1","name":"read","arguments":"{\"path\":\"README.md\"}"}
		],
		"usage":{"input_tokens":12,"output_tokens":3}
	}`))

	require.NoError(t, err)
	assert.Equal(t, "hello", result.Text)
	assert.Equal(t, []string{"thought one\n\nthought two"}, result.Thinking)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "call_1", result.ToolCalls[0].ID)
	assert.Equal(t, expectedReadToolName, result.ToolCalls[0].Name)
	assert.Equal(t, testToolPath, result.ToolCalls[0].Arguments[jsonPathKey])
	assert.Equal(t, 12, result.Usage.InputTokens)
	assert.Equal(t, 3, result.Usage.OutputTokens)
	assert.Equal(t, llm.FinishReasonToolCalls, result.FinishReason)
}

func TestParseOpenAIResponseResultMapsIncompleteFinishReasons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want llm.FinishReason
	}{
		{
			name: "max output tokens",
			body: `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},` +
				`"output":[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]}`,
			want: llm.FinishReasonLength,
		},
		{
			name: "content filter",
			body: `{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output_text":"partial"}`,
			want: llm.FinishReasonContentFilter,
		},
		{
			name: statusCompleted,
			body: `{"status":"completed","output_text":"done"}`,
			want: llm.FinishReasonStop,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseOpenAIResponseResult([]byte(testCase.body))

			require.NoError(t, err)
			assert.Equal(t, testCase.want, result.FinishReason)
		})
	}
}

func TestParseOpenAIResponseResultUsesOutputTextFallbackAndErrors(t *testing.T) {
	t.Parallel()

	result, err := parseOpenAIResponseResult([]byte(`{"output_text":"fallback text"}`))
	require.NoError(t, err)
	assert.Equal(t, "fallback text", result.Text)

	_, err = parseOpenAIResponseResult([]byte(`{"error":{"message":"bad request"}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad request")
}

func TestProviderResultFromOutputItemsUsesFallbackAndInvalidArguments(t *testing.T) {
	t.Parallel()

	result := providerResultFromOutputItems([]any{
		map[string]any{
			jsonTypeKey:      functionCallType,
			"id":             "item_1",
			jsonFunctionKey:  jsonBashToolName,
			jsonArgumentsKey: "{",
		},
	}, "fallback")

	assert.Equal(t, "fallback", result.Text)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "item_1", result.ToolCalls[0].ID)
	assert.Equal(t, expectedBashToolName, result.ToolCalls[0].Name)
	assert.Empty(t, result.ToolCalls[0].Arguments)
}

func TestResponsesPayloadReasoningModes(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("sk-test")
	setTestRequestProvider(request, testOpenAIProvider)
	setTestRequestModelID(request, "gpt-test")
	setTestRequestReasoning(request, true)
	setTestRequestThinkingLevel(request, thinkingHigh)
	payload := responsesBasePayload(request, nil, false)
	assert.Equal(t, map[string]any{
		reasoningEffortKey: thinkingHigh,
		jsonSummaryKey:     reasoningSummaryAuto,
	}, payload["reasoning"])

	setTestRequestThinkingLevel(request, thinkingXHigh)
	setTestThinkingMap(request, thinkingXHigh, "max")
	payload = responsesBasePayload(request, nil, false)
	assert.Equal(t, map[string]any{
		reasoningEffortKey: "max",
		jsonSummaryKey:     reasoningSummaryAuto,
	}, payload["reasoning"])

	setTestRequestReasoning(request, false)
	setTestRequestThinkingLevel(request, "")
	payload = responsesBasePayload(request, nil, true)
	assert.Equal(t, map[string]string{
		reasoningEffortKey: reasoningEffortNone,
		jsonSummaryKey:     reasoningSummaryAuto,
	}, payload["reasoning"])
}

func TestRequestResponsesHandlesStatusReadAndParsePaths(t *testing.T) {
	t.Parallel()

	t.Run("non streaming success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, err := writer.Write([]byte(`{"output_text":"ok"}`))
			assert.NoError(t, err)
		}))
		t.Cleanup(server.Close)

		client := &HTTPCompletionClient{client: server.Client()}
		result, err := client.requestResponses(t.Context(), server.URL, nil, map[string]any{}, false, nil)
		require.NoError(t, err)
		assert.Equal(t, "ok", result.Text)
	})

	t.Run("status error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusBadRequest)
			_, err := writer.Write([]byte(`{"error":{"message":"bad status"}}`))
			assert.NoError(t, err)
		}))
		t.Cleanup(server.Close)

		client := &HTTPCompletionClient{client: server.Client()}
		_, err := client.requestResponses(t.Context(), server.URL, nil, map[string]any{}, false, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad status")
	})
}

func TestCompleteOpenAICodexCompactsAssistantMessages(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("openai-codex", "codex-token")
	setTestRequestAPI(request, apiOpenAICodexResponses)
	setTestRequestBaseURL(request, "https://example.test")
	setTestRequestMessages(request, nil)

	assert.Equal(t, map[string]string{
		reasoningEffortKey: reasoningEffortNone,
		jsonSummaryKey:     reasoningSummaryAuto,
	}, codexReasoning(request))
}
