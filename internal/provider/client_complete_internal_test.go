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
		body        string
		api         string
		name        string
		authHeaders map[string]string
		wantHeaders map[string]string
		wantPath    string
	}{
		{
			body:        openAIResponseCompletedStream(`{"output_text":"ok"}`),
			api:         apiOpenAIResponses,
			name:        "openai responses",
			authHeaders: nil,
			wantHeaders: map[string]string{
				"Authorization": "Bearer " + testProviderAPIKey,
			},
			wantPath: "/responses",
		},
		{
			body: openAIResponseCompletedStream(`{"output_text":"ok"}`),
			api:  apiOpenAICodexResponses,
			name: "openai codex responses",
			authHeaders: map[string]string{
				codexAccountIDHeader: testProviderAccountID,
			},
			wantHeaders: map[string]string{
				"Authorization":       "Bearer " + testProviderAPIKey,
				codexBetaHeader:       codexResponsesBetaValue,
				codexOriginatorHeader: codexClientHeaderValue,
				codexUserAgentHeader:  codexClientHeaderValue,
				codexAccountIDHeader:  testProviderAccountID,
			},
			wantPath: "/codex/responses",
		},
		{
			body:        anthropicResponseStream(anthropicResponseJSON("end_turn", "ok", nil)),
			api:         apiAnthropicMessages,
			name:        "anthropic messages",
			authHeaders: nil,
			wantHeaders: map[string]string{
				"x-api-key":         testProviderAPIKey,
				"anthropic-version": "2023-06-01",
			},
			wantPath: "/v1/messages",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			httpClient, capturedRequest := testProviderHTTPClientWithCapture(t, testCase.body)
			client := &HTTPCompletionClient{client: httpClient}
			request := emptyCompletionRequest()
			request.Request.Auth.Headers = testCase.authHeaders
			request.Request.Auth.APIKey = testProviderAPIKey
			request.Request.Model.ID = "model-test"
			request.Request.Model.BaseURL = testProviderBaseURL
			setTestRequestAPI(request, testCase.api)

			response, err := client.Complete(context.Background(), request)

			require.NoError(t, err)
			assert.Equal(t, "ok", responseText(response))

			assert.Equal(t, http.MethodPost, capturedRequest.Method)
			assert.Equal(t, testCase.wantPath, capturedRequest.Path)

			for key, wantValue := range testCase.wantHeaders {
				assert.Equal(t, wantValue, capturedRequest.Headers.Get(key), key)
			}
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

type testProviderCapturedRequest struct {
	Headers http.Header
	Method  string
	Path    string
}

func testProviderHTTPClient(t *testing.T, body string) *http.Client {
	t.Helper()

	client, _ := testProviderHTTPClientWithCapture(t, body)

	return client
}

func testProviderHTTPClientWithCapture(t *testing.T, body string) (*http.Client, *testProviderCapturedRequest) {
	t.Helper()

	capturedRequest := &testProviderCapturedRequest{
		Headers: nil,
		Method:  "",
		Path:    "",
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		capturedRequest.Headers = request.Header.Clone()
		capturedRequest.Method = request.Method
		capturedRequest.Path = request.URL.Path

		assert.Equal(t, http.MethodPost, request.Method)

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}

	return client, capturedRequest
}
