package startupprofile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	assert.Equal(t, 1, countEventName(t, contents, "runtime"))
}

func TestStopFinalizesRequestedOutputsOnce(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "startup.json")
	t.Setenv(profilePathEnv, profilePath)

	profiler, err := startupprofile.Start()
	require.NoError(t, err)
	profiler.Mark("before_exit")

	require.NoError(t, profiler.Stop())
	profileContents := readProfile(t, profilePath)
	assert.Equal(t, 1, countEventName(t, profileContents, "before_exit"))
	assert.Zero(t, countEventName(t, profileContents, "first_frame"))

	require.NoError(t, os.WriteFile(profilePath, []byte("unchanged"), 0o600))
	require.NoError(t, profiler.Stop())
	assert.Equal(t, []byte("unchanged"), readProfile(t, profilePath))
}

func TestStopAfterFirstFrameDoesNotRewriteOutputs(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "startup.json")
	t.Setenv(profilePathEnv, profilePath)

	profiler, err := startupprofile.Start()
	require.NoError(t, err)
	require.NoError(t, profiler.FirstFrame())

	require.NoError(t, os.WriteFile(profilePath, []byte("unchanged"), 0o600))
	require.NoError(t, profiler.Stop())
	assert.Equal(t, []byte("unchanged"), readProfile(t, profilePath))
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

func readProfile(t *testing.T, path string) []byte {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	require.NoError(t, err)

	defer func() { require.NoError(t, root.Close()) }()

	contents, err := root.ReadFile(filepath.Base(path))
	require.NoError(t, err)

	return contents
}

func countEventName(t *testing.T, contents []byte, name string) int {
	t.Helper()

	var decoded struct {
		Events []struct {
			Name string `json:"name"`
		} `json:"events"`
	}
	require.NoError(t, json.Unmarshal(contents, &decoded))

	count := 0

	for _, profileEvent := range decoded.Events {
		if profileEvent.Name == name {
			count++
		}
	}

	return count
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
