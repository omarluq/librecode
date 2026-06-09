package core_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

func TestLibrecodeHomeUsesOverride(t *testing.T) {
	t.Setenv("LIBRECODE_HOME", " ./custom-home ")

	home, err := core.LibrecodeHome()
	require.NoError(t, err)
	assert.Equal(t, "custom-home", filepath.ToSlash(home))
}

func TestLibrecodeHomeUsesUserHome(t *testing.T) {
	t.Setenv("LIBRECODE_HOME", "")
	t.Setenv("HOME", "/tmp/librecode-test-home")

	home, err := core.LibrecodeHome()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(string(filepath.Separator), "tmp", "librecode-test-home", core.ConfigDirName), home)
}

func TestProjectConfigDir(t *testing.T) {
	t.Parallel()

	workDir := filepath.Join(string(filepath.Separator), "work")
	assert.Equal(t, filepath.Join(workDir, core.ConfigDirName), core.ProjectConfigDir(workDir))
}
