package jwtclaim_test

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/jwtclaim"
)

func TestParseClaims(t *testing.T) {
	t.Parallel()

	validToken, keyFunc := signedJWTForTest(t, map[string]any{"sub": "user-123"})

	tests := []struct {
		name    string
		token   string
		wantSub string
		wantErr bool
	}{
		{name: "valid token", token: validToken, wantSub: "user-123", wantErr: false},
		{name: "malformed token", token: "not-a-jwt", wantSub: "", wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			claims, err := jwtclaim.ParseClaims(testCase.token, keyFunc)
			if testCase.wantErr {
				require.Error(t, err)
				assert.Nil(t, claims)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantSub, claims["sub"])
		})
	}
}

func signedJWTForTest(t *testing.T, claims map[string]any) (string, jwt.Keyfunc) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyID := "test-key"
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims(claims))
	token.Header["kid"] = keyID
	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)

	jwk, err := jwkset.NewJWKFromKey(
		privateKey.Public(),
		jwkset.JWKOptions{Metadata: jwkset.JWKMetadataOptions{KID: keyID, USE: jwkset.UseSig}},
	)
	require.NoError(t, err)

	store := jwkset.NewMemoryStorage()
	require.NoError(t, store.KeyWrite(t.Context(), jwk))

	keyFunc, err := keyfunc.New(keyfunc.Options{Storage: store})
	require.NoError(t, err)

	return tokenString, keyFunc.Keyfunc
}
