//go:build windows

package tool

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindWindowsBashReturnsConfiguredPath(t *testing.T) {
	t.Setenv("LIBRECODE_BASH_PATH", "C:\\Tools\\Git\\bin\\bash.exe")

	path, err := findWindowsBash()
	require.NoError(t, err)

	assert.Equal(t, "C:\\Tools\\Git\\bin\\bash.exe", path)
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
	assert.Contains(t, candidates, windowsBashExecutable)
	assert.Contains(t, candidates, "bash")
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
