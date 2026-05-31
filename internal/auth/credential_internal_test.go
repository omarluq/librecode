package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const testOAuthAccessToken = "token"

func TestCredentialOAuthExpiredHandlesSecondAndMillisecondTimestamps(t *testing.T) {
	t.Parallel()

	oldNow := timeNow
	t.Cleanup(func() { timeNow = oldNow })
	timeNow = func() time.Time {
		return time.Unix(1_700_000_000, 0)
	}

	tests := []struct {
		name       string
		credential Credential
		expired    bool
	}{
		{
			name:       "missing expiration is not expired",
			credential: testOAuthCredentialWithExpiration(0, 0),
			expired:    false,
		},
		{
			name:       "expired seconds timestamp",
			credential: testOAuthCredentialWithExpiration(1_699_999_999, 0),
			expired:    true,
		},
		{
			name:       "future seconds timestamp",
			credential: testOAuthCredentialWithExpiration(1_700_000_001, 0),
			expired:    false,
		},
		{
			name:       "expired millisecond timestamp",
			credential: testOAuthCredentialWithExpiration(1_699_999_999_999, 0),
			expired:    true,
		},
		{
			name:       "future millisecond timestamp from expires_at fallback",
			credential: testOAuthCredentialWithExpiration(0, 1_700_000_001_000),
			expired:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expired, test.credential.oauthExpired())
		})
	}
}

func testOAuthCredentialWithExpiration(expires, expiresAt int64) Credential {
	return Credential{
		OAuth:     nil,
		Type:      CredentialTypeOAuth,
		Key:       "",
		Access:    testOAuthAccessToken,
		Refresh:   "",
		AccountID: "",
		Expires:   expires,
		ExpiresAt: expiresAt,
	}
}
