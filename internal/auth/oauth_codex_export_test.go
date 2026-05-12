package auth

import (
	"context"
	"testing"
)

func OpenAICodexClientIDForTest() string {
	return openAICodexClientID
}

func NewOpenAICodexFlowURLForTest(t *testing.T) string {
	t.Helper()
	flow, err := newOpenAICodexFlow()
	if err != nil {
		t.Fatal(err)
	}

	return flow.URL
}

func ExchangeOpenAICodexCodeForTest(ctx context.Context, code, verifier string) (*Credential, error) {
	return exchangeOpenAICodexCode(ctx, code, verifier)
}

func RefreshOpenAICodexForTest(ctx context.Context, refreshToken string) (*Credential, error) {
	return refreshOpenAICodex(ctx, refreshToken)
}

func SetOpenAICodexTokenURLForTest(t *testing.T, tokenURL string) {
	t.Helper()
	oauthTokenURLTestMu.Lock()
	oldURL := openAICodexExchangeURL
	openAICodexExchangeURL = tokenURL
	t.Cleanup(func() {
		openAICodexExchangeURL = oldURL
		oauthTokenURLTestMu.Unlock()
	})
}
