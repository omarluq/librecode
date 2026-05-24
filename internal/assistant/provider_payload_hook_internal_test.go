package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const providerPayloadHookSystemPrompt = "system"

var zeroTestTime = time.Time{}

func TestHTTPCompletionClientAppliesProviderRequestHook(t *testing.T) {
	t.Parallel()

	var requestHeader string
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestHeader = request.Header.Get("X-Test-Header")
		require.NoError(t, json.NewDecoder(request.Body).Decode(&requestBody))
		_, err := writer.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	completionRequest := providerPayloadHookRequest(server.URL)
	completionRequest.OnProviderRequest = func(
		_ context.Context,
		input providerHookInput,
	) (providerHookOutput, error) {
		payload := cloneAnyMap(input.Payload)
		payload["metadata"] = "mutated"
		headers := cloneStringMap(input.Headers)
		headers["X-Test-Header"] = "yes"

		return providerHookOutput{Payload: payload, Headers: headers}, nil
	}

	result, err := NewHTTPCompletionClient().Complete(context.Background(), completionRequest)

	require.NoError(t, err)
	assert.Equal(t, "ok", result.Text)
	assert.Equal(t, "yes", requestHeader)
	assert.Equal(t, "mutated", requestBody["metadata"])
}

func providerPayloadHookRequest(baseURL string) *CompletionRequest {
	return &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		OnToolCall:        nil,
		OnToolResult:      nil,
		ToolRegistry:      nil,
		SessionID:         "session-1",
		SystemPrompt:      providerPayloadHookSystemPrompt,
		ThinkingLevel:     "off",
		CWD:               "/work",
		Auth:              model.RequestAuth{Headers: map[string]string{}, APIKey: "test-key", Error: "", OK: true},
		Messages: []database.MessageEntity{
			{
				Timestamp: zeroTestTime,
				Role:      database.RoleUser,
				Content:   "hello",
				Provider:  "",
				Model:     "",
			},
		},
		Usage:           model.EmptyTokenUsage(),
		ProviderAttempt: 1,
		Model: model.Model{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           nil,
			Provider:         "openai",
			ID:               providerHookTestModelID,
			Name:             providerHookTestModelID,
			API:              apiOpenAICompletions,
			BaseURL:          baseURL,
			Input:            []model.InputMode{model.InputText},
			Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    0,
			MaxTokens:        0,
			Reasoning:        false,
		},
	}
}
