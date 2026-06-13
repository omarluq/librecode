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

func ExchangeOpenAICodexCodeForTest(ctx context.Context, code, verifier, tokenURL string) (*Credential, error) {
	return exchangeOpenAICodexCodeWithTokenURL(ctx, code, verifier, tokenURL)
}

func RefreshOpenAICodexForTest(ctx context.Context, refreshToken, tokenURL string) (*Credential, error) {
	return refreshOpenAICodexWithTokenURL(ctx, refreshToken, tokenURL)
}
