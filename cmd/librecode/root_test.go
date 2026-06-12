package main_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	main "github.com/omarluq/librecode/cmd/librecode"
)

//nolint:paralleltest // newRootCmd binds package-level flag variables.
func TestRootCmd_HelpFlagShowsHelp(t *testing.T) {
	cmd := main.NewRootCmdForTest()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "librecode")
}

//nolint:paralleltest // newRootCmd binds package-level flag variables.
func TestRootCmd_VersionFlagShowsVersion(t *testing.T) {
	cmd := main.NewRootCmdForTest()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}
