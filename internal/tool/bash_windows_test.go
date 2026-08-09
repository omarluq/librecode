//go:build windows

package tool

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/samber/lo"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindWindowsBashReturnsConfiguredPath(t *testing.T) {
	configuredPath := filepath.Join(t.TempDir(), windowsBashExecutable)
	require.NoError(t, os.WriteFile(configuredPath, nil, 0o600))
	t.Setenv("LIBRECODE_BASH_PATH", configuredPath)

	path, err := findWindowsBash()
	require.NoError(t, err)

	assert.Equal(t, configuredPath, path)
}

func TestWindowsBashCandidatesSkipsEmptyBaseDirs(t *testing.T) {
	t.Setenv("LIBRECODE_BASH_PATH", "")
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramFiles(x86)", "")
	t.Setenv("LOCALAPPDATA", "")

	candidates := windowsBashCandidates()

	assert.NotContains(t, candidates, filepath.Join("Git", "bin", windowsBashExecutable))
	assert.NotContains(t, candidates, filepath.Join("Git", "usr", "bin", windowsBashExecutable))
	assert.NotContains(t, candidates, filepath.Join("Programs", "Git", "bin", windowsBashExecutable))
	assert.NotContains(t, candidates, windowsBashExecutable)
	assert.NotContains(t, candidates, "bash")
}

func TestWindowsBashCandidatesIncludesConfiguredBaseDirs(t *testing.T) {
	t.Setenv("LIBRECODE_BASH_PATH", "C:\\custom\\bash.exe")
	t.Setenv("ProgramFiles", "C:\\Program Files")
	t.Setenv("ProgramFiles(x86)", "C:\\Program Files (x86)")
	t.Setenv("LOCALAPPDATA", "C:\\Users\\omar\\AppData\\Local")

	candidates := windowsBashCandidates()

	assert.Equal(t, "C:\\custom\\bash.exe", candidates[0])
	assert.True(t, lo.Contains(candidates, filepath.Join("C:\\Program Files", "Git", "bin", windowsBashExecutable)))
	assert.True(t, lo.Contains(candidates, filepath.Join("C:\\Program Files (x86)", "Git", "usr", "bin", windowsBashExecutable)))
	assert.True(t, lo.Contains(candidates, filepath.Join("C:\\Users\\omar\\AppData\\Local", "Programs", "Git", "bin", windowsBashExecutable)))
}

func TestFindWindowsBashRejectsRelativeConfiguredPath(t *testing.T) {
	pathDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(pathDir, "custom-bash.exe"), nil, 0o600))
	t.Setenv("LIBRECODE_BASH_PATH", "custom-bash.exe")
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("ProgramFiles(x86)", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("PATH", pathDir)

	_, err := findWindowsBash()
	require.Error(t, err)

	assert.True(t, errors.Is(err, errBashNotFound))
	assert.Contains(t, err.Error(), "LIBRECODE_BASH_PATH must be an absolute path")
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "custom-bash.exe", oopsErr.Context()["configured_path"])
}

func TestFindWindowsBashDoesNotUseDirectoryCandidate(t *testing.T) {
	directory := filepath.Join(t.TempDir(), windowsBashExecutable)
	require.NoError(t, os.Mkdir(directory, 0o700))
	t.Setenv("LIBRECODE_BASH_PATH", directory)
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("ProgramFiles(x86)", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	_, err := findWindowsBash()
	require.Error(t, err)

	assert.True(t, errors.Is(err, errBashNotFound))
}

func TestFindWindowsBashDoesNotFallbackToCmd(t *testing.T) {
	t.Setenv("LIBRECODE_BASH_PATH", "")
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("ProgramFiles(x86)", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	_, err := findWindowsBash()
	require.Error(t, err)

	assert.True(t, errors.Is(err, errBashNotFound))
	assert.NotContains(t, err.Error(), "cmd.exe")
}
