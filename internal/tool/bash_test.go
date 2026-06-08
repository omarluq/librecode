package tool_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

func TestBashToolRejectsBlankCommand(t *testing.T) {
	t.Parallel()

	bashTool := tool.NewBashTool(t.TempDir())
	_, err := bashTool.Bash(context.Background(), tool.BashInput{Timeout: nil, Command: " \n\t"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bash command is required")
}
