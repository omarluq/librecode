package compaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const (
	compactFileOperationTestBlank      = "   "
	compactFileOperationTestCommand    = "cat README.md"
	compactFileOperationTestPath       = "README.md"
	compactFileOperationTestReadTool   = fileActionRead
	compactFileOperationTestSourcePath = "cmd/app/main.go"
	compactFileOperationTestSummary    = "summary"
	compactFileOperationTestOrigin     = "test"
	compactFileOperationTestGuidePath  = "docs/user guide.md"
)

func TestCollectCompactionFileOperations(t *testing.T) {
	t.Parallel()

	prior := compactFileOperationCompactionEntry(t, []FileOperation{{
		EntryID: "prior",
		Action:  compactFileOperationTestReadTool,
		Path:    compactFileOperationTestPath,
		Tool:    compactFileOperationTestReadTool,
		Command: "",
	}})
	invalidPrior := compactFileOperationEntry(
		"invalid-prior",
		database.EntryTypeCompaction,
		database.RoleCompactionSummary,
		compactFileOperationTestSummary,
	)
	invalidPrior.DataJSON = `{"details":{"file_operations":"not an operation list"}}`
	read := compactFileOperationToolEntry(
		"read-1",
		compactFileOperationTestReadTool,
		compactFileOperationPathArgsJSON(),
	)
	duplicateRead := compactFileOperationToolEntry(
		"read-2",
		compactFileOperationTestReadTool,
		compactFileOperationPathArgsJSON(),
	)
	write := compactFileOperationToolEntry("write-1", "write", `{"path":"internal/app.go"}`)
	find := compactFileOperationToolEntry("find-1", "find", `{"pattern":"internal/**/*.go"}`)
	bash := compactFileOperationToolEntry(
		"bash-1",
		"bash",
		`{"command":"cat go.mod && sed -i 's/a/b/' internal/app.go"}`,
	)
	unknown := compactFileOperationToolEntry("unknown-1", "unknown", `{"path":"ignored.go"}`)
	badArgs := compactFileOperationToolEntry("bad-args", compactFileOperationTestReadTool, `{`)
	missingPath := compactFileOperationToolEntry("missing-path", compactFileOperationTestReadTool, `{}`)

	operations := CollectFileOperations([]database.EntryEntity{
		prior,
		invalidPrior,
		*read,
		*duplicateRead,
		*write,
		*find,
		*bash,
		*unknown,
		*badArgs,
		*missingPath,
	})

	assertCompactionFileOperation(
		t,
		operations,
		compactFileOperationTestReadTool,
		compactFileOperationTestPath,
		compactFileOperationTestReadTool,
	)
	assertCompactionFileOperation(
		t,
		operations,
		compactFileOperationTestReadTool,
		compactFileOperationTestSourcePath,
		compactFileOperationTestReadTool,
	)
	assertCompactionFileOperation(t, operations, fileActionModified, "internal/app.go", "write")
	assertCompactionFileOperation(t, operations, compactFileOperationTestReadTool, "internal/**/*.go", "find")
	assertCompactionFileOperation(t, operations, fileActionModified, "go.mod", "bash")
	assertCompactionFileOperation(t, operations, fileActionModified, "internal/app.go", "bash")

	mutatingBash := bashFileOperations(
		compactFileOperationToolEntry("bash-2", "bash", `{"command":"sed -i 's/a/b/' internal/app.go"}`),
		map[string]any{"command": "sed -i 's/a/b/' internal/app.go"},
	)
	require.Len(t, mutatingBash, 1)
	assert.Equal(t, fileActionModified, mutatingBash[0].Action)
	assert.Len(t, operations, 6)
}

func TestAppendUniqueCompactionFileOperation(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}
	operations := []FileOperation{}
	operations = appendUniqueFileOperation(operations, seen, nil)
	operations = appendUniqueFileOperation(operations, seen, &FileOperation{
		EntryID: "",
		Action:  " ",
		Path:    "x.go",
		Tool:    "",
		Command: "",
	})
	operations = appendUniqueFileOperation(operations, seen, &FileOperation{
		EntryID: "",
		Action:  compactFileOperationTestReadTool,
		Path:    " ",
		Tool:    "",
		Command: "",
	})
	operations = appendUniqueFileOperation(operations, seen, &FileOperation{
		EntryID: "",
		Action:  " " + compactFileOperationTestReadTool + " ",
		Path:    " " + compactFileOperationTestPath + " ",
		Tool:    " " + compactFileOperationTestReadTool + " ",
		Command: " " + compactFileOperationTestCommand + " ",
	})
	operations = appendUniqueFileOperation(operations, seen, &FileOperation{
		EntryID: "",
		Action:  compactFileOperationTestReadTool,
		Path:    compactFileOperationTestPath,
		Tool:    compactFileOperationTestReadTool,
		Command: compactFileOperationTestCommand,
	})

	require.Len(t, operations, 1)
	assert.Equal(t, FileOperation{
		EntryID: "",
		Action:  compactFileOperationTestReadTool,
		Path:    compactFileOperationTestPath,
		Tool:    compactFileOperationTestReadTool,
		Command: compactFileOperationTestCommand,
	}, operations[0])
}

func TestShellCommandParsing(t *testing.T) {
	t.Parallel()

	tests := append(shellCommandParsingTestCases(), shellCommandParsingExtraTestCases()...)
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.wantPaths, shellCommandPathTokens(testCase.command))
			assert.Equal(t, testCase.wantWrite, shellCommandLooksMutating(testCase.command))
		})
	}
}

type shellCommandParsingTestCase struct {
	command   string
	name      string
	wantPaths []string
	wantWrite bool
}

func shellCommandParsingTestCases() []shellCommandParsingTestCase {
	return []shellCommandParsingTestCase{
		{
			name:      "redirection",
			command:   "echo ok > out.txt",
			wantPaths: []string{"out.txt"},
			wantWrite: true,
		},
		{
			name:      "copy command",
			command:   "cp a.txt b.txt",
			wantPaths: []string{"a.txt", "b.txt"},
			wantWrite: true,
		},
		{
			name:      "sed in place",
			command:   "sed -i 's/a/b/' internal/app.go",
			wantPaths: []string{"internal/app.go"},
			wantWrite: true,
		},
		{
			name:      "perl in place with suffix",
			command:   "perl -i.bak -pe s/a/b/ file.go",
			wantPaths: []string{"file.go"},
			wantWrite: true,
		},
		{
			name:      "quoted path",
			command:   `cat "docs/user guide.md" './internal/app.go'`,
			wantPaths: []string{compactFileOperationTestGuidePath, "./internal/app.go"},
			wantWrite: false,
		},
		{
			name:      "escaped space path",
			command:   `cat docs/user\ guide.md`,
			wantPaths: []string{compactFileOperationTestGuidePath},
			wantWrite: false,
		},
		{
			name:      "pipe commands",
			command:   `cat go.mod | grep librecode > result.txt`,
			wantPaths: []string{"go.mod", "result.txt"},
			wantWrite: true,
		},
		{
			name:      "dynamic path skipped",
			command:   `cat "$HOME/go.mod" ./static.go`,
			wantPaths: []string{"./static.go"},
			wantWrite: false,
		},
		{
			name:      "quoted command name is not command",
			command:   `echo 'sed -i example' README.md`,
			wantPaths: []string{"README.md"},
			wantWrite: false,
		},
		{
			name:      "empty command",
			command:   compactFileOperationTestBlank,
			wantPaths: []string{},
			wantWrite: false,
		},
		{
			name:      "invalid command",
			command:   `cat 'unterminated`,
			wantPaths: []string{},
			wantWrite: false,
		},
		{
			name:      "read command",
			command:   compactFileOperationTestCommand,
			wantPaths: []string{compactFileOperationTestPath},
			wantWrite: false,
		},
	}
}

func shellCommandParsingExtraTestCases() []shellCommandParsingTestCase {
	return []shellCommandParsingTestCase{
		{
			name:      "single quoted path",
			command:   `cat 'docs/user guide.md'`,
			wantPaths: []string{compactFileOperationTestGuidePath},
			wantWrite: false,
		},
		{
			name:      "shell expansion skipped",
			command:   `cat $TARGET ./static.go`,
			wantPaths: []string{"./static.go"},
			wantWrite: false,
		},
	}
}

func TestFileOperationFormattingHelpers(t *testing.T) {
	t.Parallel()

	command := strings.Repeat("x", 200)
	assert.Len(t, truncateCommand(command), 160)
	assert.True(t, strings.HasSuffix(truncateCommand(command), "..."))

	summary := AppendFileOperationsSummary(
		"summary\n\n"+fileOperationsHeader+"\n- read: stale",
		[]FileOperation{{
			EntryID: "",
			Action:  compactFileOperationTestReadTool,
			Path:    compactFileOperationTestPath,
			Tool:    compactFileOperationTestReadTool,
			Command: "",
		}},
	)
	assert.Equal(t, "summary\n\n"+fileOperationsHeader+"\n- read: README.md (via read)", summary)
	assert.Equal(t, "summary", StripFileOperationsSummary(summary))
}

func compactFileOperationCompactionEntry(
	t *testing.T,
	operations []FileOperation,
) database.EntryEntity {
	t.Helper()

	entry := compactFileOperationEntry(
		"prior-compaction",
		database.EntryTypeCompaction,
		database.RoleCompactionSummary,
		compactFileOperationTestSummary,
	)
	data := entryData{Details: map[string]any{FileOperationsKey: operations}}
	encoded, err := json.Marshal(data)
	require.NoError(t, err)

	entry.DataJSON = string(encoded)

	return entry
}

func compactFileOperationToolEntry(entryID, toolName, argsJSON string) *database.EntryEntity {
	entry := compactFileOperationEntry(entryID, database.EntryTypeMessage, database.RoleToolResult, "ok")
	entry.ToolName = toolName
	entry.ToolArgsJSON = argsJSON

	return &entry
}

func compactFileOperationEntry(
	entryID string,
	entryType database.EntryType,
	role database.Role,
	content string,
) database.EntryEntity {
	return testEntry(entryID, entryType, role, content)
}

func compactFileOperationPathArgsJSON() string {
	return `{"path":"` + compactFileOperationTestSourcePath + `"}`
}

func assertCompactionFileOperation(
	t *testing.T,
	operations []FileOperation,
	action string,
	path string,
	tool string,
) {
	t.Helper()

	for index := range operations {
		operation := operations[index]
		if operation.Action == action && operation.Path == path && operation.Tool == tool {
			return
		}
	}

	t.Fatalf("missing file operation action=%q path=%q tool=%q in %#v", action, path, tool, operations)
}
