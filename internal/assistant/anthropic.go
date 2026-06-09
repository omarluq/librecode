package assistant

import "strings"

func requestUsesAnthropicOAuth(request *CompletionRequest) bool {
	return request != nil &&
		(request.Model.Provider == "anthropic-claude" || isAnthropicOAuthToken(request.Auth.APIKey))
}

func isAnthropicOAuthToken(apiKey string) bool {
	return strings.HasPrefix(apiKey, "sk-ant-oat")
}
