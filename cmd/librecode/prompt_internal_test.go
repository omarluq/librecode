package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptMessageReadsStdinBelowLimit(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("hello\n"))

	message, err := promptMessage(cmd, nil)
	require.NoError(t, err)
	assert.Equal(t, "hello", message)
}

func TestPromptMessageRejectsStdinAboveLimit(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(strings.Repeat("a", int(promptStdinLimitBytes)+1)))

	message, err := promptMessage(cmd, nil)
	require.Error(t, err)
	assert.Empty(t, message)
	assert.Contains(t, err.Error(), "prompt stdin exceeds limit")
}
