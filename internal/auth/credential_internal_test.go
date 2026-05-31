package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	testOAuthAccessToken = "token"
	testFlatAccessToken  = "flat-access"
	testMapAccessToken   = "map-access"
	testRefreshToken     = "refresh"
	testAccessKey        = "access"
	testMapRefreshToken  = "map-refresh"
	testFlatRefreshToken = "flat-refresh"
)

func TestCredentialOAuthExpiredHandlesSecondAndMillisecondTimestamps(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name       string
		credential Credential
		expired    bool
	}{
		{
			name:       "missing expiration",
			expired:    false,
			credential: testOAuthCredentialWithExpiration(0, 0),
		},
		{
			name:       "expired seconds",
			expired:    true,
			credential: testOAuthCredentialWithExpiration(now.Add(-time.Hour).Unix(), 0),
		},
		{
			name:       "future seconds",
			expired:    false,
			credential: testOAuthCredentialWithExpiration(now.Add(time.Hour).Unix(), 0),
		},
		{
			name:       "expired milliseconds",
			expired:    true,
			credential: testOAuthCredentialWithExpiration(now.Add(-time.Hour).UnixMilli(), 0),
		},
		{
			name:       "future expires_at fallback",
			expired:    false,
			credential: testOAuthCredentialWithExpiration(0, now.Add(time.Hour).UnixMilli()),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expired, test.credential.oauthExpired())
		})
	}
}

func TestCredentialAPIKeyValueResolvesStoredEnvReference(t *testing.T) {
	t.Setenv("STORED_PROVIDER_KEY", "env-secret")

	credential := apiKeyCredential("STORED_PROVIDER_KEY")
	value, found := credential.apiKeyValue()
	assert.True(t, found)
	assert.Equal(t, "env-secret", value)
}

func TestCredentialAPIKeyValueOAuth(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name       string
		value      string
		credential Credential
		found      bool
	}{
		{
			name:       "flat access",
			value:      testFlatAccessToken,
			credential: oauthCredential(testFlatAccessToken, nil, now.Add(time.Hour).Unix()),
			found:      true,
		},
		{
			name:  "map access",
			value: testMapAccessToken,
			credential: oauthCredential(
				"",
				map[string]string{testAccessKey: testMapAccessToken},
				now.Add(time.Hour).Unix(),
			),
			found: true,
		},
		{
			name:       "expired access",
			value:      "",
			credential: oauthCredential("expired-access", nil, now.Add(-time.Hour).Unix()),
			found:      false,
		},
		{
			name:       "unknown type",
			value:      "",
			credential: unknownCredential(),
			found:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			value, found := test.credential.apiKeyValue()
			assert.Equal(t, test.value, value)
			assert.Equal(t, test.found, found)
		})
	}
}

func TestCredentialHasSecretMaterial(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		credential Credential
		hasSecret  bool
	}{
		{name: "api key", credential: apiKeyCredential("secret"), hasSecret: true},
		{name: "api key whitespace", credential: apiKeyCredential(" \t\n "), hasSecret: false},
		{name: "oauth flat refresh", credential: oauthCredential("", nil, 0), hasSecret: true},
		{
			name:       "oauth map access",
			credential: oauthCredential("", map[string]string{testAccessKey: testMapAccessToken}, 0),
			hasSecret:  true,
		},
		{name: "unknown type", credential: unknownCredential(), hasSecret: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.hasSecret, test.credential.hasSecretMaterial())
		})
	}
}

func TestCredentialOAuthAccessAndRefreshPreferFlatFields(t *testing.T) {
	t.Parallel()

	credential := oauthCredential(testFlatAccessToken, map[string]string{
		testAccessKey:    testMapAccessToken,
		testRefreshToken: testMapRefreshToken,
	}, 0)
	credential.Refresh = testFlatRefreshToken

	assert.Equal(t, testFlatAccessToken, credential.oauthAccess())
	assert.Equal(t, testFlatRefreshToken, credential.oauthRefresh())
}

func testOAuthCredentialWithExpiration(expires, expiresAt int64) Credential {
	credential := oauthCredential(testOAuthAccessToken, nil, expires)
	credential.ExpiresAt = expiresAt

	return credential
}

func apiKeyCredential(key string) Credential {
	return Credential{
		OAuth:     nil,
		Type:      CredentialTypeAPIKey,
		Key:       key,
		Access:    "",
		Refresh:   "",
		AccountID: "",
		Expires:   0,
		ExpiresAt: 0,
	}
}

func oauthCredential(access string, tokenMap map[string]string, expires int64) Credential {
	return Credential{
		OAuth:     tokenMap,
		Type:      CredentialTypeOAuth,
		Key:       "",
		Access:    access,
		Refresh:   testRefreshToken,
		AccountID: "",
		Expires:   expires,
		ExpiresAt: 0,
	}
}

func unknownCredential() Credential {
	return Credential{
		OAuth:     nil,
		Type:      CredentialType("custom"),
		Key:       "secret",
		Access:    "access",
		Refresh:   testRefreshToken,
		AccountID: "",
		Expires:   0,
		ExpiresAt: 0,
	}
}
