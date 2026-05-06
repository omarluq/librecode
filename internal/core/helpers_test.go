package core_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const frontmatterDelimiter = "---"

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func newOutsideTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp(filepath.Clean("/tmp"), "librecode-core-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(dir))
	})

	return dir
}
