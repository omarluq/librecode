package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testInvalidJSON = "invalid json"

func TestLoginAnthropicWithCodeRejectsMalformedPublicCode(t *testing.T) {
	t.Parallel()

	credential, err := LoginAnthropicWithCode(t.Context(), "invalid")
	require.Error(t, err)
	assert.Nil(t, credential)
	assert.Contains(t, err.Error(), "code#state")
}

func TestAnthropicAPIKeyRefreshesExpiredCredential(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)

		var payload map[string]string
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		assert.Equal(t, grantTypeRefreshToken, payload[grantTypeKey])
		assert.Equal(t, "old-refresh", payload["refresh_token"])

		_, err := writer.Write([]byte(`{
			"access_token":"sk-ant-oat-refreshed",
			"refresh_token":"new-refresh",
			"expires_in":3600
		}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	credential := oauthCredential("expired", nil, time.Now().Add(-time.Hour).UnixMilli())
	credential.Refresh = "old-refresh"

	refreshed, err := refreshAnthropicWithTokenURL(t.Context(), credential.Refresh, server.URL)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-oat-refreshed", refreshed.Access)
	assert.Equal(t, "new-refresh", refreshed.Refresh)
	assert.Equal(t, "sk-ant-oat-refreshed", apiKeyFromCredentialForTest(refreshed, ""))
}

func TestDecodeAnthropicTokenRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: testInvalidJSON, body: `{`},
		{name: "missing refresh", body: `{"access_token":"access","expires_in":3600}`},
		{name: "missing access", body: `{"refresh_token":"refresh","expires_in":3600}`},
		{name: "missing expiry", body: `{"access_token":"access","refresh_token":"refresh"}`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			credential, err := decodeAnthropicToken([]byte(testCase.body))
			require.Error(t, err)
			assert.Nil(t, credential)
		})
	}
}

func TestPostAnthropicTokenErrors(t *testing.T) {
	t.Parallel()

	credential, err := requestAnthropicToken(
		t.Context(),
		":// bad-url",
		map[string]string{grantTypeKey: grantTypeRefreshToken},
	)
	require.Error(t, err)
	assert.Nil(t, credential)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, writeErr := writer.Write([]byte(`{"error":"denied"}`))
		assert.NoError(t, writeErr)
	}))
	defer server.Close()

	credential, err = requestAnthropicToken(
		t.Context(),
		server.URL,
		map[string]string{grantTypeKey: grantTypeRefreshToken},
	)
	require.Error(t, err)
	assert.Nil(t, credential)
	assert.Contains(t, err.Error(), "denied")
}

func TestOpenAICodexOAuthTopLevelAndCallback(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	credential, err := LoginOpenAICodex(ctx, nil)
	require.Error(t, err)
	assert.Nil(t, credential)

	server, err := startOpenAICodexCallbackServer("expected-state")
	if err != nil {
		t.Skipf("callback port unavailable: %v", err)
	}
	defer server.Close(context.Background())

	status := getCallbackStatusForTest(t, "/auth/callback?state=wrong&code=abc")
	assert.Equal(t, http.StatusBadRequest, status)

	_, err = server.Wait(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state mismatch")
}

func TestOpenAICodexCallbackReceivesCodeAndHandlesContext(t *testing.T) {
	t.Parallel()

	server, err := startOpenAICodexCallbackServer("state")
	if err != nil {
		t.Skipf("callback port unavailable: %v", err)
	}
	defer server.Close(context.Background())

	status := getCallbackStatusForTest(t, "/auth/callback?state=state&code=auth-code")
	assert.Equal(t, http.StatusOK, status)

	code, err := server.Wait(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "auth-code", code)

	waiting := &openAICodexCallbackServer{
		server:     &http.Server{ReadHeaderTimeout: oauthCallbackTimeout},
		codes:      make(chan callbackResult),
		codePrefix: "codex",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	code, err = waiting.Wait(ctx)
	require.Error(t, err)
	assert.Empty(t, code)
}

func TestWriteOAuthHTML(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	writeOAuthHTML(recorder, http.StatusTeapot, "<done>")

	assert.Equal(t, http.StatusTeapot, recorder.Code)
	assert.Equal(t, "text/html; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Contains(t, recorder.Body.String(), "&lt;done&gt;")
	assert.NotContains(t, recorder.Body.String(), "<done>")
}

func TestDecodeOpenAICodexTokenRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: testInvalidJSON, body: `{`},
		{name: "missing refresh", body: `{"access_token":"access","expires_in":3600}`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			credential, err := decodeOpenAICodexToken(t.Context(), []byte(testCase.body), testOpenAICodexParser("acct"))
			require.Error(t, err)
			assert.Nil(t, credential)
		})
	}
}

func TestOpenAICodexAPIKeyRefreshBranches(t *testing.T) {
	t.Parallel()

	apiCredential := apiKeyCredential("api-key")
	credential, apiKey, err := openAICodexAPIKey(t.Context(), &apiCredential)
	require.NoError(t, err)
	assert.Equal(t, &apiCredential, credential)
	assert.Empty(t, apiKey)

	missingRefresh := oauthCredential("", nil, 0)
	missingRefresh.Refresh = ""
	credential, apiKey, err = openAICodexAPIKey(t.Context(), &missingRefresh)
	require.NoError(t, err)
	assert.Equal(t, &missingRefresh, credential)
	assert.Empty(t, apiKey)
}

func TestAccountIDFromJWTErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		parseJWT openAICodexJWTParser
		name     string
		token    string
	}{
		{name: "invalid segments", token: "one.two", parseJWT: testFailingOpenAICodexParser},
		{name: "invalid base64", token: "one.%%%.three", parseJWT: testFailingOpenAICodexParser},
		{name: testInvalidJSON, token: "one.e30.three", parseJWT: testFailingOpenAICodexParser},
		{name: "missing auth claim", token: "token", parseJWT: testOpenAICodexClaimsParser(map[string]any{})},
		{
			name:     "missing account id",
			token:    "token",
			parseJWT: testOpenAICodexClaimsParser(map[string]any{openAICodexJWTClaim: map[string]any{}}),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			accountID, err := accountIDFromJWT(t.Context(), testCase.token, testCase.parseJWT)
			require.Error(t, err)
			assert.Empty(t, accountID)
		})
	}
}

func getCallbackStatusForTest(t *testing.T, path string) int {
	t.Helper()

	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://"+openAICodexCallback+path,
		http.NoBody,
	)
	require.NoError(t, err)

	client := &http.Client{Timeout: time.Second}
	response, err := client.Do(request)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, response.Body.Close())
	}()

	return response.StatusCode
}

func apiKeyFromCredentialForTest(credential *Credential, fallback string) string {
	if credential == nil {
		return fallback
	}

	if access := credential.oauthAccess(); access != "" {
		return access
	}

	return fallback
}

func testOpenAICodexClaimsParser(claims map[string]any) openAICodexJWTParser {
	return func(context.Context, string) (map[string]any, error) {
		return claims, nil
	}
}

func testFailingOpenAICodexParser(context.Context, string) (map[string]any, error) {
	return nil, errors.New("invalid jwt")
}
