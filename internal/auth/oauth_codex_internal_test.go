package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/golang-jwt/jwt/v5"
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
			"access_token": "access-token",
			"refresh_token": "refresh-token",
			"expires_in": 3600
		}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	credential, err := exchangeOpenAICodexCodeWithTokenURLAndParser(
		t.Context(),
		"auth-code",
		"verifier",
		server.URL,
		testOpenAICodexParser("acct_123"),
	)
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
			"access_token": "access-token",
			"refresh_token": "new-refresh",
			"expires_in": 3600
		}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	credential, err := refreshOpenAICodexWithTokenURLAndParser(
		t.Context(),
		"old-refresh",
		server.URL,
		testOpenAICodexParser("acct_refresh"),
	)
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

func TestParseOpenAICodexJWTWithJWKSetURL(t *testing.T) {
	t.Parallel()

	token, jwkSetJSON := signedOpenAICodexJWTForTest(t, "acct_signed")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write(jwkSetJSON)
		assert.NoError(t, err)
	}))
	defer server.Close()

	claims, err := parseOpenAICodexJWTWithJWKSetURL(t.Context(), token, server.URL)

	require.NoError(t, err)

	authClaims, ok := claims[openAICodexJWTClaim].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "acct_signed", authClaims["chatgpt_account_id"])
}

func TestParseOpenAICodexJWTWithJWKSetURLRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	_, jwkSetJSON := signedOpenAICodexJWTForTest(t, "acct_signed")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write(jwkSetJSON)
		assert.NoError(t, err)
	}))
	defer server.Close()

	claims, err := parseOpenAICodexJWTWithJWKSetURL(t.Context(), "not-a-jwt", server.URL)

	require.Error(t, err)
	assert.Nil(t, claims)
}

func testOpenAICodexParser(accountID string) openAICodexJWTParser {
	return func(context.Context, string) (map[string]any, error) {
		return map[string]any{
			openAICodexJWTClaim: map[string]any{
				"chatgpt_account_id": accountID,
			},
		}, nil
	}
}

func newOpenAICodexFlowURLForTest(t *testing.T) string {
	t.Helper()

	flow, err := newOpenAICodexFlow()
	require.NoError(t, err)

	return flow.URL
}

func signedOpenAICodexJWTForTest(t *testing.T, accountID string) (tokenString string, jwkSetJSON []byte) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyID := "openai-codex-test-key"
	claims := jwt.MapClaims{
		openAICodexJWTClaim: map[string]any{
			"chatgpt_account_id": accountID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = keyID
	tokenString, err = token.SignedString(privateKey)
	require.NoError(t, err)

	jwk, err := jwkset.NewJWKFromKey(
		privateKey.Public(),
		jwkset.JWKOptions{Metadata: jwkset.JWKMetadataOptions{KID: keyID, USE: jwkset.UseSig}},
	)
	require.NoError(t, err)

	store := jwkset.NewMemoryStorage()
	require.NoError(t, store.KeyWrite(t.Context(), jwk))
	jwkSetJSON, err = store.JSONPublic(t.Context())
	require.NoError(t, err)

	return tokenString, jwkSetJSON
}
