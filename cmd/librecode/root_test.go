package main_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	main "github.com/omarluq/librecode/cmd/librecode"
)

func TestRootCmd_ShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := main.NewRootCmdForTest()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "librecode")
}
