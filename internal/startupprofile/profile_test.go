package startupprofile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/startupprofile"
)

const profilePathEnv = "LIBRECODE_STARTUP_PROFILE"

func TestSpanCompletionIsIdempotent(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "startup.json")
	t.Setenv(profilePathEnv, profilePath)

	profiler, err := startupprofile.Start()
	require.NoError(t, err)

	finish := profiler.Span("runtime")
	finish()
	finish()

	require.NoError(t, profiler.FirstFrame())

	contents := readProfile(t, profilePath)
	assert.Equal(t, 1, countEventName(contents, "runtime"))
}

func TestReportReplacesPermissiveFileMode(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "startup.json")
	createPermissiveProfile(t, profilePath)
	t.Setenv(profilePathEnv, profilePath)

	profiler, err := startupprofile.Start()
	require.NoError(t, err)
	require.NoError(t, profiler.FirstFrame())

	info, err := os.Stat(profilePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func readProfile(t *testing.T, path string) string {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	require.NoError(t, err)

	defer func() { require.NoError(t, root.Close()) }()

	contents, err := root.ReadFile(filepath.Base(path))
	require.NoError(t, err)

	return string(contents)
}

func countEventName(contents, name string) int {
	needle := `"name": "` + name + `"`

	return strings.Count(contents, needle)
}

func createPermissiveProfile(t *testing.T, path string) {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	require.NoError(t, err)

	defer func() { require.NoError(t, root.Close()) }()

	file, err := root.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	require.NoError(t, file.Chmod(0o644))
	require.NoError(t, file.Close())
}
