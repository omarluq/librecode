package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolPayloadReadsStdinBelowLimit(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(`{"command":"pwd"}`))

	payload, err := toolPayload(cmd, []string{"-"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"command":"pwd"}`, string(payload))
}

func TestToolPayloadRejectsStdinAboveLimit(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(strings.Repeat("a", int(toolJSONStdinLimitBytes)+1)))

	payload, err := toolPayload(cmd, []string{"-"})
	require.Error(t, err)
	assert.Empty(t, payload)
	assert.Contains(t, err.Error(), "tool JSON stdin exceeds limit")
}
