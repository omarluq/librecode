package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

const (
	providerHookTestModelID         = "gpt-test"
	providerHookPayloadKey          = "payload"
	providerPayloadHookSystemPrompt = "system"
	providerPayloadHookHeaderValue  = "yes"
)

func TestHTTPCompletionClientAppliesProviderRequestHook(t *testing.T) {
	t.Parallel()

	captures := make(chan providerHookCapture, 1)
	server := newProviderHookCaptureServer(
		t,
		captures,
		"X-Test-Header",
		`{"choices":[{"message":{"content":"ok"}}]}`,
	)
	t.Cleanup(server.Close)
	completionRequest := providerPayloadHookRequest(server.URL)
	completionRequest.OnProviderRequest = func(
		_ context.Context,
		input *llm.HookInput,
	) (llm.HookOutput, error) {
		payload := cloneAnyMap(input.Payload)
		payload["metadata"] = "mutated"
		headers := cloneStringMap(input.Headers)
		headers["X-Test-Header"] = providerPayloadHookHeaderValue

		return llm.HookOutput{Payload: payload, Headers: headers}, nil
	}

	result, err := NewHTTPCompletionClient().Complete(context.Background(), completionRequest)

	require.NoError(t, err)

	capture := <-captures
	require.NoError(t, capture.Err)
	assert.Equal(t, "ok", responseText(result))
	assert.Equal(t, providerPayloadHookHeaderValue, capture.Header)
	assert.Equal(t, "mutated", capture.Body["metadata"])
}

func TestHTTPCompletionClientAppliesProviderRequestHookToOpenAIResponses(t *testing.T) {
	t.Parallel()

	captures := make(chan providerHookCapture, 1)
	server := newProviderHookCaptureServer(t, captures, "X-Responses-Hook", `{"output_text":"ok"}`)
	t.Cleanup(server.Close)
	completionRequest := providerPayloadHookRequest(server.URL)
	setTestRequestAPI(completionRequest, apiOpenAIResponses)
	completionRequest.OnProviderRequest = func(
		_ context.Context,
		input *llm.HookInput,
	) (llm.HookOutput, error) {
		payload := cloneAnyMap(input.Payload)
		payload["responses_metadata"] = "mutated"
		headers := cloneStringMap(input.Headers)
		headers["X-Responses-Hook"] = strconv.Itoa(input.Attempt)

		return llm.HookOutput{Payload: payload, Headers: headers}, nil
	}

	result, err := NewHTTPCompletionClient().Complete(context.Background(), completionRequest)

	require.NoError(t, err)

	capture := <-captures
	require.NoError(t, capture.Err)
	assert.Equal(t, "ok", responseText(result))
	assert.Equal(t, "1", capture.Header)
	assert.Equal(t, "mutated", capture.Body["responses_metadata"])
}

func TestApplyProviderRequestHookObservesMutatedRequests(t *testing.T) {
	t.Parallel()

	request := providerPayloadHookRequest("https://example.test")
	calls := []string{}
	request.OnProviderObserve = func(_ context.Context, input *llm.HookInput) {
		calls = append(calls, "observe:"+strconv.Itoa(input.Attempt))
	}
	request.OnProviderRequest = func(
		_ context.Context,
		input *llm.HookInput,
	) (llm.HookOutput, error) {
		calls = append(calls, "mutate:"+strconv.Itoa(input.Attempt))

		return llm.HookOutput{Payload: input.Payload, Headers: input.Headers}, nil
	}

	_, err := applyProviderRequestHook(
		context.Background(),
		request,
		map[string]any{providerHookPayloadKey: true},
		map[string]string{},
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"mutate:1", "observe:1"}, calls)
}

func TestApplyProviderRequestHookHandlesNilRequest(t *testing.T) {
	t.Parallel()

	result, err := applyProviderRequestHook(
		context.Background(),
		nil,
		map[string]any{providerHookPayloadKey: true},
		map[string]string{"X-Test": providerPayloadHookHeaderValue},
	)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{providerHookPayloadKey: true}, result.Payload)
	assert.Equal(t, map[string]string{"X-Test": providerPayloadHookHeaderValue}, result.Headers)
}

type providerHookCapture struct {
	Err    error
	Body   map[string]any
	Header string
}

func newProviderHookCaptureServer(
	t *testing.T,
	captures chan<- providerHookCapture,
	header string,
	response string,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		capture := providerHookCapture{
			Err:    nil,
			Body:   map[string]any{},
			Header: request.Header.Get(header),
		}
		if err := json.NewDecoder(request.Body).Decode(&capture.Body); err != nil {
			capture.Err = err
			captures <- capture

			return
		}

		if _, err := writer.Write([]byte(response)); err != nil {
			capture.Err = err
		}

		captures <- capture
	}))
}

func providerPayloadHookRequest(baseURL string) *CompletionRequest {
	return &CompletionRequest{
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ExecuteTools:      nil,
		OnEvent:           nil,
		Request: llm.Request{
			ProviderOptions: map[string]any{"cwd": "/work"},
			Auth:            llm.Auth{Headers: map[string]string{}, APIKey: "test-key"},
			SystemPrompt:    providerPayloadHookSystemPrompt,
			ThinkingLevel:   "off",
			SessionID:       "session-1",
			Messages:        []llm.Message{llm.TextMessage(llm.RoleUser, "hello")},
			Tools:           nil,
			Model: llm.ModelRef{
				Metadata:         nil,
				ThinkingLevelMap: nil,
				Provider:         testOpenAIProvider,
				ID:               providerHookTestModelID,
				API:              apiOpenAICompletions,
				BaseURL:          baseURL,
				MaxTokens:        0,
				ContextWindow:    0,
				Reasoning:        false,
			},
			Usage:        llm.EmptyUsage(),
			DisableTools: false,
		},
		ProviderAttempt: 1,
	}
}
