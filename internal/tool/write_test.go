package tool_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

const writeTestEmptyPath = "empty.txt"

func TestWriteToolValidatesPathAndContent(t *testing.T) {
	t.Parallel()

	writeTool := tool.NewWriteTool(t.TempDir())
	empty := ""

	tests := []struct {
		input tool.WriteInput
		name  string
		want  string
	}{
		{
			name:  "blank path",
			input: tool.WriteInput{Path: " \n\t", Content: &empty},
			want:  "write path is required",
		},
		{
			name:  "missing content",
			input: tool.WriteInput{Path: writeTestEmptyPath, Content: nil},
			want:  "write content is required",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := writeTool.Write(context.Background(), testCase.input)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.want)
		})
	}
}

func TestWriteToolAllowsIntentionalEmptyContent(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	writeTool := tool.NewWriteTool(cwd)
	content := ""

	result, err := writeTool.Write(context.Background(), tool.WriteInput{
		Path:    "nested/" + writeTestEmptyPath,
		Content: &content,
	})

	require.NoError(t, err)
	assert.Contains(t, result.Text(), "Successfully wrote 0 bytes")
	outputPath := filepath.Join(cwd, "nested", writeTestEmptyPath)
	written, err := os.ReadFile(filepath.Clean(outputPath))
	require.NoError(t, err)
	assert.Empty(t, string(written))
}
