//go:build windows

package tool

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindWindowsBashReturnsConfiguredPath(t *testing.T) {
	t.Setenv("LIBRECODE_BASH_PATH", `C:\\Tools\\Git\\bin\\bash.exe`)

	path, err := findWindowsBash()
	require.NoError(t, err)

	assert.Equal(t, `C:\\Tools\\Git\\bin\\bash.exe`, path)
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
