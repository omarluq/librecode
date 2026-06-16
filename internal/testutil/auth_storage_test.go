package testutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestNewAuthStorage(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		"openai": {
			OAuth:     nil,
			Type:      auth.CredentialTypeAPIKey,
			Key:       "secret",
			Access:    "",
			Refresh:   "",
			AccountID: "",
			Expires:   0,
			ExpiresAt: 0,
		},
	})

	credential, found := storage.Get("openai")
	require.True(t, found)
	assert.Equal(t, auth.CredentialTypeAPIKey, credential.Type)
	assert.Equal(t, "secret", credential.Key)
}
