// Package testutil provides shared test helpers for internal packages.
package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
)

// authFileMode is the file permission for auth credential files.
const authFileMode = 0o600

// NewAuthStorage creates file-backed auth storage seeded with credentials.
func NewAuthStorage(t *testing.T, credentials map[string]auth.Credential) *auth.Storage {
	t.Helper()

	content, err := json.MarshalIndent(credentials, "", "  ")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(path, content, authFileMode))

	storage, err := auth.NewStorage(t.Context(), auth.NewFileBackend(path))
	require.NoError(t, err)

	return storage
}
