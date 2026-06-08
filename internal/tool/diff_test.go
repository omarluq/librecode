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

const editDiffMaxLinesForTest = 400

type editDiffBehaviorCase struct {
	name              string
	content           string
	wantMessageString string
	wantContains      []string
	wantNotContains   []string
	edits             []tool.Replacement
	wantFirstLine     int
	maxDiffLineCount  int
	wantTruncated     bool
}

func TestEditToolDiffBehavior(t *testing.T) {
	t.Parallel()

	for _, testCase := range editDiffBehaviorCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			editTool, relativePath := newEditDiffFixture(t, testCase.content)
			result, err := editTool.Edit(context.Background(), tool.EditInput{
				Path:  relativePath,
				Edits: testCase.edits,
			})

			require.NoError(t, err)
			diff := requireDiffDetail(t, result)
			assert.Equal(t, testCase.wantFirstLine, requireIntDetail(t, result, "firstChangedLine"))
			assert.Equal(t, testCase.wantTruncated, requireBoolDetail(t, result, "diffTruncated"))
			for _, substring := range testCase.wantContains {
				assert.Contains(t, diff, substring)
			}
			for _, substring := range testCase.wantNotContains {
				assert.NotContains(t, diff, substring)
			}
			if testCase.maxDiffLineCount > 0 {
				assert.LessOrEqual(t, len(strings.Split(diff, "\n")), testCase.maxDiffLineCount)
			}
			if testCase.wantMessageString != "" {
				assert.Contains(t, result.Text(), testCase.wantMessageString)
			}
		})
	}
}

func editDiffBehaviorCases() []editDiffBehaviorCase {
	largeOldContent := numberedLines(700)
	largeNewContent := strings.ReplaceAll(largeOldContent, "line ", "changed line ")

	return []editDiffBehaviorCase{
		{
			name:    "distant edits use separate unified hunks",
			content: numberedLines(30),
			edits: []tool.Replacement{
				{OldText: "line 03\n", NewText: "line 03 changed\n"},
				{OldText: "line 27\n", NewText: "line 27 changed\n"},
			},
			wantContains: []string{
				"--- before",
				"+++ after",
				"@@ -1,7 +1,7 @@",
				"@@ -23,8 +23,8 @@",
				"-line 03",
				"+line 03 changed",
				"-line 27",
				"+line 27 changed",
			},
			wantNotContains:   []string{"line 15"},
			wantMessageString: "",
			wantFirstLine:     3,
			maxDiffLineCount:  0,
			wantTruncated:     false,
		},
		{
			name:              "reports first changed line at start of file",
			content:           numberedLines(5),
			edits:             []tool.Replacement{{OldText: "line 01\n", NewText: "hello\n"}},
			wantContains:      []string{"@@ -1,5 +1,5 @@", "+hello"},
			wantNotContains:   []string{},
			wantMessageString: "",
			wantFirstLine:     1,
			maxDiffLineCount:  0,
			wantTruncated:     false,
		},
		{
			name:              "truncates large diffs",
			content:           largeOldContent,
			edits:             []tool.Replacement{{OldText: largeOldContent, NewText: largeNewContent}},
			wantContains:      []string{},
			wantNotContains:   []string{},
			wantMessageString: "diff truncated",
			wantFirstLine:     1,
			maxDiffLineCount:  editDiffMaxLinesForTest,
			wantTruncated:     true,
		},
		{
			name:    "insert only hunk reports new line",
			content: numberedLines(12),
			edits: []tool.Replacement{
				{OldText: "line 09\nline 10\n", NewText: "line 09\ninserted\nline 10\n"},
			},
			wantContains:      []string{"@@ -6,7 +6,8 @@", "+inserted"},
			wantNotContains:   []string{},
			wantMessageString: "",
			wantFirstLine:     10,
			maxDiffLineCount:  0,
			wantTruncated:     false,
		},
	}
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
		if _, err := fmt.Fprintf(&builder, "line %02d\n", line); err != nil {
			panic(fmt.Sprintf("failed to build numbered lines: %v", err))
		}
	}

	return builder.String()
}
