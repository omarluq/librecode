package startupprofile_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/startupprofile"
)

const (
	profilePathEnv = "LIBRECODE_STARTUP_PROFILE"
	tracePathEnv   = "LIBRECODE_STARTUP_TRACE"
	cpuPathEnv     = "LIBRECODE_STARTUP_CPU_PROFILE"
)

type profileReport struct {
	Events []profileEvent `json:"events"`
}

type profileEvent struct {
	Name     string `json:"name"`
	Duration int64  `json:"duration_ns,omitempty"`
}

func TestStartAndFinalizeOutputs(t *testing.T) {
	tests := []struct {
		name string
		env  string
		ext  string
	}{
		{name: "report", env: profilePathEnv, ext: ".json"},
		{name: "runtime trace", env: tracePathEnv, ext: ".trace"},
		{name: "CPU profile", env: cpuPathEnv, ext: ".pprof"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearProfileEnvironment(t)
			path := filepath.Join(t.TempDir(), "startup"+test.ext)
			t.Setenv(test.env, path)

			profiler, err := startupprofile.Start()
			require.NoError(t, err)
			profiler.Mark("configured")
			finish := profiler.Span("runtime")

			time.Sleep(time.Millisecond)
			finish()
			finish()
			require.NoError(t, profiler.FirstFrame())

			contents := readProfile(t, path)
			assert.NotEmpty(t, contents)

			if test.env == profilePathEnv {
				decoded := decodeReport(t, contents)
				assert.Equal(t, []string{"startup", "configured", "runtime", "first_frame"}, eventNames(decoded))
				assert.Positive(t, decoded.Events[2].Duration)
			}
		})
	}

	t.Run("disabled", func(t *testing.T) {
		clearProfileEnvironment(t)

		profiler, err := startupprofile.Start()
		require.NoError(t, err)

		profiler.Mark("ignored")
		profiler.Span("ignored")()
		require.NoError(t, profiler.FirstFrame())
		require.NoError(t, profiler.Stop())
	})
}

func TestNilProfilerOperationsAreNoops(t *testing.T) {
	t.Parallel()

	var profiler *startupprofile.Profiler
	profiler.Mark("ignored")
	profiler.Span("ignored")()
	require.NoError(t, profiler.FirstFrame())
	require.NoError(t, profiler.Stop())
}

func TestContextRoundTrip(t *testing.T) {
	t.Parallel()

	profiler, err := startupprofile.Start()
	require.NoError(t, err)

	ctx := startupprofile.Context(context.Background(), profiler)

	assert.Same(t, profiler, startupprofile.FromContext(ctx))
	assert.Nil(t, startupprofile.FromContext(context.Background()))
}

func TestFinalizationIsIdempotent(t *testing.T) {
	tests := []struct {
		finalize func(*startupprofile.Profiler) error
		name     string
	}{
		{name: "stop", finalize: (*startupprofile.Profiler).Stop},
		{name: "first frame", finalize: (*startupprofile.Profiler).FirstFrame},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearProfileEnvironment(t)
			profilePath := filepath.Join(t.TempDir(), "startup.json")
			t.Setenv(profilePathEnv, profilePath)

			profiler, err := startupprofile.Start()
			require.NoError(t, err)
			profiler.Mark("before_finish")
			require.NoError(t, test.finalize(profiler))

			first := readProfile(t, profilePath)

			profiler.Mark("after_finish")
			profiler.Span("after_finish")()
			require.NoError(t, test.finalize(profiler))
			assert.Equal(t, first, readProfile(t, profilePath))
			assert.Zero(t, countEventName(t, first, "after_finish"))
		})
	}
}

func TestStartReportsOutputCreationErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		wantErr string
	}{
		{name: "trace", env: tracePathEnv, wantErr: "create startup trace"},
		{name: "CPU", env: cpuPathEnv, wantErr: "create startup CPU profile"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearProfileEnvironment(t)
			t.Setenv(test.env, filepath.Join(t.TempDir(), "missing", "profile"))

			profiler, err := startupprofile.Start()
			assert.Nil(t, profiler)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestStopReportsReportWriteError(t *testing.T) {
	clearProfileEnvironment(t)
	path := filepath.Join(t.TempDir(), "removed", "startup.json")
	require.NoError(t, os.Mkdir(filepath.Dir(path), 0o700))
	t.Setenv(profilePathEnv, path)

	profiler, err := startupprofile.Start()
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Dir(path)))

	require.ErrorContains(t, profiler.Stop(), "write startup report")
}

func TestReportReplacesPermissiveFileMode(t *testing.T) {
	clearProfileEnvironment(t)
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

func clearProfileEnvironment(t *testing.T) {
	t.Helper()

	for _, name := range []string{profilePathEnv, tracePathEnv, cpuPathEnv} {
		t.Setenv(name, "")
	}
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

func decodeReport(t *testing.T, contents []byte) profileReport {
	t.Helper()

	var decoded profileReport
	require.NoError(t, json.Unmarshal(contents, &decoded))

	return decoded
}

func eventNames(decoded profileReport) []string {
	names := make([]string, 0, len(decoded.Events))
	for _, profileEvent := range decoded.Events {
		names = append(names, profileEvent.Name)
	}

	return names
}

func countEventName(t *testing.T, contents []byte, name string) int {
	t.Helper()

	count := 0

	for _, profileEvent := range decodeReport(t, contents).Events {
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
