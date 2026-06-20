package jwtclaim_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/jwtclaim"
)

func TestParseUnverifiedClaims(t *testing.T) {
	t.Parallel()

	token := jwtForClaims(t, map[string]any{"sub": "user-123"})

	claims, err := jwtclaim.ParseUnverifiedClaims(token)

	require.NoError(t, err)
	assert.Equal(t, "user-123", claims["sub"])
}

func TestParseUnverifiedClaimsRejectsMalformedToken(t *testing.T) {
	t.Parallel()

	claims, err := jwtclaim.ParseUnverifiedClaims("not-a-jwt")

	require.Error(t, err)
	assert.Nil(t, claims)
}

func jwtForClaims(t *testing.T, claims map[string]any) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payloadBytes, err := json.Marshal(claims)
	require.NoError(t, err)

	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signature := base64.RawURLEncoding.EncodeToString([]byte("signature"))

	return header + "." + payload + "." + signature
}
