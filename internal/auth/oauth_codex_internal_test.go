package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAICodexFlowMatchesOAuthShape(t *testing.T) {
	t.Parallel()

	authURL := newOpenAICodexFlowURLForTest(t)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	query := parsed.Query()

	assert.Equal(t, "code", query.Get("response_type"))
	assert.Equal(t, openAICodexClientID, query.Get("client_id"))
	assert.Equal(t, "http://localhost:1455/auth/callback", query.Get("redirect_uri"))
	assert.Equal(t, "openid profile email offline_access", query.Get("scope"))
	assert.Equal(t, "S256", query.Get("code_challenge_method"))
	assert.NotEmpty(t, query.Get("code_challenge"))
	assert.NotEmpty(t, query.Get("state"))
	assert.Equal(t, "true", query.Get("id_token_add_organizations"))
	assert.Equal(t, "true", query.Get("codex_cli_simplified_flow"))
	assert.Equal(t, "pi", query.Get("originator"))
}

func TestExchangeOpenAICodexCodeSendsPKCEForm(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		assert.NoError(t, request.ParseForm())
		assert.Equal(t, "authorization_code", request.Form.Get("grant_type"))
		assert.Equal(t, openAICodexClientID, request.Form.Get("client_id"))
		assert.Equal(t, "auth-code", request.Form.Get("code"))
		assert.Equal(t, "verifier", request.Form.Get("code_verifier"))
		assert.Equal(t, "http://localhost:1455/auth/callback", request.Form.Get("redirect_uri"))

		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write([]byte(`{
			"access_token": "` + testOpenAICodexJWT(t, "acct_123") + `",
			"refresh_token": "refresh-token",
			"expires_in": 3600
		}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	credential, err := exchangeOpenAICodexCodeWithTokenURL(t.Context(), "auth-code", "verifier", server.URL)
	require.NoError(t, err)
	assert.Equal(t, CredentialTypeOAuth, credential.Type)
	assert.Equal(t, "acct_123", credential.AccountID)
	assert.Equal(t, "refresh-token", credential.Refresh)
	assert.Greater(t, credential.Expires, time.Now().UnixMilli())
}

func TestRefreshOpenAICodexSendsRefreshForm(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.NoError(t, request.ParseForm())
		assert.Equal(t, "refresh_token", request.Form.Get("grant_type"))
		assert.Equal(t, "old-refresh", request.Form.Get("refresh_token"))
		assert.Equal(t, openAICodexClientID, request.Form.Get("client_id"))

		_, err := writer.Write([]byte(`{
			"access_token": "` + testOpenAICodexJWT(t, "acct_refresh") + `",
			"refresh_token": "new-refresh",
			"expires_in": 3600
		}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	credential, err := refreshOpenAICodexWithTokenURL(t.Context(), "old-refresh", server.URL)
	require.NoError(t, err)
	assert.Equal(t, "acct_refresh", credential.AccountID)
	assert.Equal(t, "new-refresh", credential.Refresh)
}

func TestOpenAICodexTokenErrorIncludesBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_, err := writer.Write([]byte(`{"error":"blocked"}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	_, err := refreshOpenAICodexWithTokenURL(t.Context(), "refresh", server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token request failed")
	assert.Contains(t, err.Error(), "blocked")
}

func testOpenAICodexJWT(t *testing.T, accountID string) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payloadBytes, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
		},
	})
	require.NoError(t, err)

	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signature := base64.RawURLEncoding.EncodeToString([]byte("signature"))

	return strings.Join([]string{header, payload, signature}, ".")
}

func newOpenAICodexFlowURLForTest(t *testing.T) string {
	t.Helper()

	flow, err := newOpenAICodexFlow()
	require.NoError(t, err)

	return flow.URL
}
