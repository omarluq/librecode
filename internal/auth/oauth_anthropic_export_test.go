package auth

import (
	"context"
	"testing"
)

func AnthropicClientIDForTest() string {
	return anthropicClientID
}

func NewAnthropicFlowURLForTest(t *testing.T) string {
	t.Helper()

	flow, err := newAnthropicFlow()
	if err != nil {
		t.Fatal(err)
	}

	return flow.URL
}

func AnthropicLoginURLForTest() (string, error) {
	return AnthropicLoginURL()
}

func LoginAnthropicWithCodeForTest(ctx context.Context, code, tokenURL string) (*Credential, error) {
	return loginAnthropicWithCode(ctx, code, tokenURL)
}

func RefreshAnthropicForTest(ctx context.Context, refreshToken, tokenURL string) (*Credential, error) {
	return refreshAnthropicWithTokenURL(ctx, refreshToken, tokenURL)
}

func IsAnthropicOAuthAccessTokenForTest(token string) bool {
	return isAnthropicOAuthAccessToken(token)
}
