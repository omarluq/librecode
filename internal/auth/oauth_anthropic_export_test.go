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

func LoginAnthropicWithCodeForTest(ctx context.Context, code string) (*Credential, error) {
	return LoginAnthropicWithCode(ctx, code)
}

func RefreshAnthropicForTest(ctx context.Context, refreshToken string) (*Credential, error) {
	return refreshAnthropic(ctx, refreshToken)
}

func SetAnthropicTokenURLForTest(t *testing.T, url string) {
	t.Helper()
	oauthTokenURLTestMu.Lock()
	oldURL := anthropicTokenURL
	anthropicTokenURL = url
	t.Cleanup(func() {
		anthropicTokenURL = oldURL
		oauthTokenURLTestMu.Unlock()
	})
}

func IsAnthropicOAuthAccessTokenForTest(token string) bool {
	return isAnthropicOAuthAccessToken(token)
}
