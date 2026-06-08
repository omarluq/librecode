package tool_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

func TestEditToolDiffUsesUnifiedHunksForDistantEdits(t *testing.T) {
	t.Parallel()

	editTool, relativePath := newEditDiffFixture(t, numberedLines(30))

	result, err := editTool.Edit(context.Background(), tool.EditInput{
		Path: relativePath,
		Edits: []tool.Replacement{
			{OldText: "line 03\n", NewText: "line 03 changed\n"},
			{OldText: "line 27\n", NewText: "line 27 changed\n"},
		},
	})

	require.NoError(t, err)
	diff := requireDiffDetail(t, result)
	assert.Equal(t, 3, requireIntDetail(t, result, "firstChangedLine"))
	assert.False(t, requireBoolDetail(t, result, "diffTruncated"))
	assert.Contains(t, diff, "--- before")
	assert.Contains(t, diff, "+++ after")
	assert.Contains(t, diff, "@@ -1,7 +1,7 @@")
	assert.Contains(t, diff, "@@ -23,8 +23,8 @@")
	assert.Contains(t, diff, "-line 03")
	assert.Contains(t, diff, "+line 03 changed")
	assert.Contains(t, diff, "-line 27")
	assert.Contains(t, diff, "+line 27 changed")
	assert.NotContains(t, diff, "line 15")
}

func TestEditToolDiffReportsFirstChangedLine(t *testing.T) {
	t.Parallel()

	editTool, relativePath := newEditDiffFixture(t, numberedLines(5))

	result, err := editTool.Edit(context.Background(), tool.EditInput{
		Path: relativePath,
		Edits: []tool.Replacement{
			{OldText: "line 01\n", NewText: "hello\n"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, 1, requireIntDetail(t, result, "firstChangedLine"))
	diff := requireDiffDetail(t, result)
	assert.Contains(t, diff, "@@ -1,5 +1,5 @@")
	assert.Contains(t, diff, "+hello")
}

func TestEditToolDiffTruncatesLargeDiff(t *testing.T) {
	t.Parallel()

	oldContent := numberedLines(700)
	newContent := strings.ReplaceAll(oldContent, "line ", "changed line ")
	editTool, relativePath := newEditDiffFixture(t, oldContent)

	result, err := editTool.Edit(context.Background(), tool.EditInput{
		Path: relativePath,
		Edits: []tool.Replacement{
			{OldText: oldContent, NewText: newContent},
		},
	})

	require.NoError(t, err)
	assert.True(t, requireBoolDetail(t, result, "diffTruncated"))
	assert.LessOrEqual(t, len(strings.Split(requireDiffDetail(t, result), "\n")), 400)
	assert.Contains(t, result.Text(), "diff truncated")
}

func newEditDiffFixture(t *testing.T, content string) (editTool *tool.EditTool, relativePath string) {
	t.Helper()

	cwd := t.TempDir()
	relativePath = filepath.Join("src", "main.txt")
	absolutePath := filepath.Join(cwd, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absolutePath), 0o750))
	require.NoError(t, os.WriteFile(absolutePath, []byte(content), 0o600))

	return tool.NewEditTool(cwd), relativePath
}

func requireDiffDetail(t *testing.T, result tool.Result) string {
	t.Helper()

	value, ok := result.Details["diff"].(string)
	require.Truef(t, ok, "diff detail should be string: %#v", result.Details["diff"])

	return value
}

func requireIntDetail(t *testing.T, result tool.Result, key string) int {
	t.Helper()

	value, ok := result.Details[key].(int)
	require.Truef(t, ok, "detail %q should be int: %#v", key, result.Details[key])

	return value
}

func requireBoolDetail(t *testing.T, result tool.Result, key string) bool {
	t.Helper()

	value, ok := result.Details[key].(bool)
	require.Truef(t, ok, "detail %q should be bool: %#v", key, result.Details[key])

	return value
}

func numberedLines(count int) string {
	var builder strings.Builder
	for line := 1; line <= count; line++ {
		_, _ = fmt.Fprintf(&builder, "line %02d\n", line)
	}

	return builder.String()
}
