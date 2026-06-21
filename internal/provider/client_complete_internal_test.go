package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestHTTPCompletionClientGenerate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		configure func(*llm.Request)
		name      string
	}{
		{
			name: "uses request",
			configure: func(request *llm.Request) {
				request.Auth = llm.Auth{Headers: nil, APIKey: testProviderAPIKey}
				request.Model.ID = "gpt-test"
				request.Model.API = apiOpenAICompletions
				request.Model.BaseURL = testProviderBaseURL
			},
		},
		{name: "uses default request", configure: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := &HTTPCompletionClient{client: testProviderHTTPClient(t, openAIChatTextStream("ok"))}

			var request *llm.Request

			if testCase.configure != nil {
				configured := llm.EmptyRequest()
				testCase.configure(&configured)
				request = &configured
			}

			response, err := client.Generate(context.Background(), request)

			require.NoError(t, err)
			require.NotNil(t, response)
			assert.Equal(t, "ok", responseText(response))
		})
	}
}

func TestHTTPCompletionClientCompleteRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		request     *CompletionRequest
		name        string
		wantErrText string
	}{
		{name: "nil request", request: nil, wantErrText: "completion request is nil"},
		{
			name: "unsupported api",
			request: func() *CompletionRequest {
				request := emptyCompletionRequest()
				setTestRequestAPI(request, "unsupported-api")

				return request
			}(),
			wantErrText: "provider api is not implemented",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := NewHTTPCompletionClient()
			response, err := client.Complete(context.Background(), testCase.request)

			require.Error(t, err)
			assert.Nil(t, response)
			assert.Contains(t, err.Error(), testCase.wantErrText)
		})
	}
}

func TestHTTPCompletionClientCompleteDispatchesProviderAPIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		body string
		api  string
		name string
	}{
		{
			name: "openai responses",
			api:  apiOpenAIResponses,
			body: openAIResponseCompletedStream(`{"output_text":"ok"}`),
		},
		{
			name: "openai codex responses",
			api:  apiOpenAICodexResponses,
			body: openAIResponseCompletedStream(`{"output_text":"ok"}`),
		},
		{
			name: "anthropic messages",
			api:  apiAnthropicMessages,
			body: anthropicResponseStream(anthropicResponseJSON("end_turn", "ok", nil)),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := &HTTPCompletionClient{client: testProviderHTTPClient(t, testCase.body)}
			request := emptyCompletionRequest()
			request.Request.Auth.APIKey = testProviderAPIKey
			request.Request.Model.ID = "model-test"
			request.Request.Model.BaseURL = testProviderBaseURL
			setTestRequestAPI(request, testCase.api)

			response, err := client.Complete(context.Background(), request)

			require.NoError(t, err)
			assert.Equal(t, "ok", responseText(response))
		})
	}
}

func TestHTTPCompletionClientCompleteDefaultsToOpenAIChat(t *testing.T) {
	t.Parallel()

	requestBody := openAIChatTextStream("ok")
	client := &HTTPCompletionClient{client: testProviderHTTPClient(t, requestBody)}

	request := emptyCompletionRequest()
	request.Request.Auth.APIKey = testProviderAPIKey
	request.Request.Model.ID = "gpt-test"
	request.Request.Model.BaseURL = testProviderBaseURL

	response, err := client.Complete(context.Background(), request)

	require.NoError(t, err)
	assert.Equal(t, "ok", responseText(response))
}

func testProviderHTTPClient(t *testing.T, body string) *http.Client {
	t.Helper()

	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, "POST", request.Method)

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
}
