package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fileToolsTestFile = "file.txt"
	fileToolsOldText  = "two"
)

func TestLSToolLS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup       func(t *testing.T, root string) string
		input       LSInput
		name        string
		wantText    string
		wantErrText string
	}{
		{
			setup: func(t *testing.T, root string) string {
				t.Helper()

				return root
			},
			input:       LSInput{Limit: nil, Path: ""},
			name:        "empty directory",
			wantText:    "(empty directory)",
			wantErrText: "",
		},
		{
			setup: func(t *testing.T, root string) string {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), privateFileMode))
				require.NoError(t, os.WriteFile(filepath.Join(root, "A.txt"), []byte("a"), privateFileMode))
				require.NoError(t, os.Mkdir(filepath.Join(root, "dir"), privateDirMode))

				return root
			},
			input:       LSInput{Limit: new(2), Path: ""},
			name:        "sorted entries with directory suffix and limit notice",
			wantText:    "A.txt\nb.txt\n\n[2 entries limit reached",
			wantErrText: "",
		},
		{
			setup: func(t *testing.T, root string) string {
				t.Helper()

				return root
			},
			input:       LSInput{Limit: nil, Path: "missing"},
			name:        "missing path",
			wantText:    "",
			wantErrText: "path not found",
		},
		{
			setup: func(t *testing.T, root string) string {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(root, fileToolsTestFile), []byte("x"), privateFileMode))

				return root
			},
			input:       LSInput{Limit: nil, Path: fileToolsTestFile},
			name:        "file path is rejected",
			wantText:    "",
			wantErrText: "not a directory",
		},
		{
			setup: func(t *testing.T, root string) string {
				t.Helper()

				return root
			},
			input:       LSInput{Limit: new(0), Path: ""},
			name:        "invalid limit",
			wantText:    "",
			wantErrText: "ls limit must be greater than zero",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := testCase.setup(t, t.TempDir())

			result, err := NewLSTool(root).LS(context.Background(), testCase.input)
			assertToolResult(t, result, err, testCase.wantText, testCase.wantErrText)
		})
	}
}

func TestLSToolLSRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewLSTool(t.TempDir()).LS(ctx, LSInput{Limit: nil, Path: ""})

	require.ErrorIs(t, err, context.Canceled)
}

func TestWriteToolExecuteAndContext(t *testing.T) {
	t.Parallel()

	t.Run("execute rejects wrong JSON type", func(t *testing.T) {
		t.Parallel()

		result, err := NewWriteTool(t.TempDir()).Execute(
			context.Background(),
			testArguments(`{"path":42,"content":"x"}`),
		)

		require.Error(t, err)
		assert.Empty(t, result.Text())
	})

	t.Run("write respects canceled context", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		content := "x"
		_, err := NewWriteTool(t.TempDir()).Write(ctx, WriteInput{Content: &content, Path: fileToolsTestFile})

		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestEditToolEdit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup       func(t *testing.T, root string)
		name        string
		wantText    string
		wantFile    string
		wantErrText string
		input       EditInput
	}{
		{
			setup:       func(t *testing.T, _ string) { t.Helper() },
			input:       EditInput{Path: " ", Edits: []Replacement{}},
			name:        "blank path",
			wantText:    "",
			wantFile:    "",
			wantErrText: "edit path is required",
		},
		{
			setup: func(t *testing.T, _ string) { t.Helper() },
			input: EditInput{
				Path:  "missing.txt",
				Edits: []Replacement{{OldText: "old", NewText: "new"}},
			},
			name:        "missing file",
			wantText:    "",
			wantFile:    "",
			wantErrText: "read file",
		},
		{
			setup: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.WriteFile(
					filepath.Join(root, fileToolsTestFile),
					[]byte("one\r\n"+fileToolsOldText+"\r\n"),
					privateFileMode,
				))
			},
			input: EditInput{
				Path:  fileToolsTestFile,
				Edits: []Replacement{{OldText: fileToolsOldText, NewText: "TWO"}},
			},
			name:        "successful edit preserves line endings",
			wantText:    "Successfully replaced 1 block(s) in " + fileToolsTestFile + ".",
			wantFile:    "one\r\nTWO\r\n",
			wantErrText: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			testCase.setup(t, root)

			result, err := NewEditTool(root).Edit(context.Background(), testCase.input)
			assertToolResult(t, result, err, testCase.wantText, testCase.wantErrText)

			if testCase.wantFile == "" {
				return
			}

			written, err := readFileToolsTestOutput(root)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantFile, string(written))
		})
	}
}

func TestEditToolExecuteRejectsWrongJSONType(t *testing.T) {
	t.Parallel()

	_, err := NewEditTool(t.TempDir()).Execute(context.Background(), testArguments(`{"path":42,"edits":[]}`))

	require.Error(t, err)
}

func assertToolResult(t *testing.T, result Result, err error, wantText, wantErrText string) {
	t.Helper()

	if wantErrText != "" {
		require.Error(t, err)
		assert.Contains(t, err.Error(), wantErrText)

		return
	}

	require.NoError(t, err)
	assert.Contains(t, result.Text(), wantText)
}

func readFileToolsTestOutput(root string) ([]byte, error) {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open file tools test root: %w", err)
	}

	content, readErr := directory.ReadFile(fileToolsTestFile)
	closeErr := directory.Close()

	if readErr != nil {
		return nil, fmt.Errorf("read file tools test output: %w", readErr)
	}

	if closeErr != nil {
		return nil, fmt.Errorf("close file tools test root: %w", closeErr)
	}

	return content, nil
}
