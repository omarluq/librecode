package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAnthropicFlowIncludesPKCE(t *testing.T) {
	t.Parallel()

	authURL := newAnthropicFlowURLForTest(t)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	query := parsed.Query()

	assert.Equal(t, anthropicClientID, query.Get("client_id"))
	assert.Equal(t, "code", query.Get("response_type"))
	assert.Equal(t, "http://localhost:53692/callback", query.Get("redirect_uri"))
	assert.Equal(t, "S256", query.Get("code_challenge_method"))
	assert.NotEmpty(t, query.Get("code_challenge"))
	assert.NotEmpty(t, query.Get("state"))
}

func TestAnthropicLoginURL(t *testing.T) {
	t.Parallel()

	loginURL, err := AnthropicLoginURL()
	require.NoError(t, err)
	assert.Contains(t, loginURL, "code_challenge=")
}

func TestLoginAnthropicWithCode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "application/json", request.Header.Get("Content-Type"))

		var payload map[string]string
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		assert.Equal(t, "authorization_code", payload["grant_type"])
		assert.Equal(t, "code", payload["code"])
		assert.Equal(t, "state", payload["state"])
		assert.Equal(t, "state", payload["code_verifier"])
		assert.Equal(t, "http://localhost:53692/callback", payload["redirect_uri"])

		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write([]byte(`{
			"access_token":"sk-ant-oat-access",
			"refresh_token":"refresh",
			"expires_in":3600
		}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	credential, err := loginAnthropicWithCode(t.Context(), "code#state", server.URL)
	require.NoError(t, err)
	assert.Equal(t, CredentialTypeOAuth, credential.Type)
	assert.Equal(t, "sk-ant-oat-access", credential.Access)
}

func TestLoginAnthropicWithCodeRejectsInvalidCode(t *testing.T) {
	t.Parallel()

	credential, err := loginAnthropicWithCode(t.Context(), "code-only", "https://example.invalid/token")

	require.Error(t, err)
	assert.Nil(t, credential)
	assert.Contains(t, err.Error(), "code#state")
}

func TestAnthropicAPIKey(t *testing.T) {
	t.Parallel()

	validCredential := oauthCredential("sk-ant-oat-valid", nil, time.Now().Add(time.Hour).UnixMilli())
	credential, apiKey, err := anthropicAPIKey(t.Context(), &validCredential)
	require.NoError(t, err)
	assert.Equal(t, &validCredential, credential)
	assert.Equal(t, "sk-ant-oat-valid", apiKey)

	apiCredential := apiKeyCredential("sk-ant-api03-key")
	credential, apiKey, err = anthropicAPIKey(t.Context(), &apiCredential)
	require.NoError(t, err)
	assert.Equal(t, &apiCredential, credential)
	assert.Empty(t, apiKey)

	refreshOnly := oauthCredential("", nil, 0)
	refreshOnly.Refresh = ""
	credential, apiKey, err = anthropicAPIKey(t.Context(), &refreshOnly)
	require.NoError(t, err)
	assert.Equal(t, &refreshOnly, credential)
	assert.Empty(t, apiKey)
}

func TestRequestAnthropicToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "application/json", request.Header.Get("Content-Type"))

		var payload map[string]string
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		assert.Equal(t, "refresh_token", payload["grant_type"])
		assert.Equal(t, anthropicClientID, payload["client_id"])

		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write([]byte(`{
			"access_token":"sk-ant-oat-access",
			"refresh_token":"refresh",
			"expires_in":3600
		}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	credential, err := refreshAnthropicWithTokenURL(t.Context(), "refresh", server.URL)
	require.NoError(t, err)
	assert.Equal(t, CredentialTypeOAuth, credential.Type)
	assert.Equal(t, "sk-ant-oat-access", credential.Access)
	assert.Equal(t, "refresh", credential.Refresh)
	assert.Greater(t, credential.Expires, time.Now().UnixMilli())
}

func TestIsAnthropicOAuthAccessToken(t *testing.T) {
	t.Parallel()

	assert.True(t, isAnthropicOAuthAccessToken("sk-ant-oat-access"))
	assert.False(t, isAnthropicOAuthAccessToken("sk-ant-api03-access"))
}

func newAnthropicFlowURLForTest(t *testing.T) string {
	t.Helper()

	flow, err := newAnthropicFlow()
	require.NoError(t, err)

	return flow.URL
}
