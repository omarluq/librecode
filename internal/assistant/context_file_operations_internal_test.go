package assistant

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
	compactFileOperationTestReadTool   = jsonReadToolName
	compactFileOperationTestSourcePath = "cmd/app/main.go"
	compactFileOperationTestSummary    = "summary"
	compactFileOperationTestOrigin     = "test"
)

func TestCollectCompactionFileOperations(t *testing.T) {
	t.Parallel()

	prior := compactFileOperationCompactionEntry(t, []compactionFileOperation{{
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

	operations := collectCompactionFileOperations([]database.EntryEntity{
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
	assertCompactionFileOperation(t, operations, compactionFileActionModified, "internal/app.go", "write")
	assertCompactionFileOperation(t, operations, compactFileOperationTestReadTool, "internal/**/*.go", "find")
	assertCompactionFileOperation(t, operations, compactFileOperationTestReadTool, "go.mod", "bash")
	assertCompactionFileOperation(t, operations, compactFileOperationTestReadTool, "internal/app.go", "bash")
	mutatingBash := bashFileOperations(
		compactFileOperationToolEntry("bash-2", "bash", `{"command":"sed -i 's/a/b/' internal/app.go"}`),
		map[string]any{"command": "sed -i 's/a/b/' internal/app.go"},
	)
	require.Len(t, mutatingBash, 1)
	assert.Equal(t, compactionFileActionModified, mutatingBash[0].Action)
	assert.Len(t, operations, 6)
}

func TestAppendUniqueCompactionFileOperation(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}
	operations := []compactionFileOperation{}
	operations = appendUniqueCompactionFileOperation(operations, seen, nil)
	operations = appendUniqueCompactionFileOperation(operations, seen, &compactionFileOperation{
		EntryID: "",
		Action:  " ",
		Path:    "x.go",
		Tool:    "",
		Command: "",
	})
	operations = appendUniqueCompactionFileOperation(operations, seen, &compactionFileOperation{
		EntryID: "",
		Action:  compactFileOperationTestReadTool,
		Path:    " ",
		Tool:    "",
		Command: "",
	})
	operations = appendUniqueCompactionFileOperation(operations, seen, &compactionFileOperation{
		EntryID: "",
		Action:  " " + compactFileOperationTestReadTool + " ",
		Path:    " " + compactFileOperationTestPath + " ",
		Tool:    " " + compactFileOperationTestReadTool + " ",
		Command: " " + compactFileOperationTestCommand + " ",
	})
	operations = appendUniqueCompactionFileOperation(operations, seen, &compactionFileOperation{
		EntryID: "",
		Action:  compactFileOperationTestReadTool,
		Path:    compactFileOperationTestPath,
		Tool:    compactFileOperationTestReadTool,
		Command: compactFileOperationTestCommand,
	})

	require.Len(t, operations, 1)
	assert.Equal(t, compactionFileOperation{
		EntryID: "",
		Action:  compactFileOperationTestReadTool,
		Path:    compactFileOperationTestPath,
		Tool:    compactFileOperationTestReadTool,
		Command: compactFileOperationTestCommand,
	}, operations[0])
}

func TestShellCommandLooksMutating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		command string
		name    string
		want    bool
	}{
		{name: "redirection", command: "echo ok > out.txt", want: true},
		{name: "copy command", command: "cp a.txt b.txt", want: true},
		{name: "sed in place", command: "sed -i 's/a/b/' file.go", want: true},
		{name: "perl in place with suffix", command: "perl -i.bak -pe s/a/b/ file.go", want: true},
		{name: "quoted rm is not command", command: "echo remove this README.md", want: false},
		{name: "sed text without in place", command: "echo 'sed -i example' README.md", want: false},
		{name: "empty command", command: compactFileOperationTestBlank, want: false},
		{name: "read command", command: compactFileOperationTestCommand, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, shellCommandLooksMutating(testCase.command))
		})
	}
}

func TestFileOperationFormattingHelpers(t *testing.T) {
	t.Parallel()

	command := strings.Repeat("x", 200)
	assert.Len(t, truncateCompactionOperationCommand(command), 160)
	assert.True(t, strings.HasSuffix(truncateCompactionOperationCommand(command), "..."))

	summary := appendFileOperationsSummary(
		"summary\n\n"+compactionFileOperationsHeader+"\n- read: stale",
		[]compactionFileOperation{{
			EntryID: "",
			Action:  compactFileOperationTestReadTool,
			Path:    compactFileOperationTestPath,
			Tool:    compactFileOperationTestReadTool,
			Command: "",
		}},
	)
	assert.Equal(t, "summary\n\n"+compactionFileOperationsHeader+"\n- read: README.md (via read)", summary)
	assert.Equal(t, "summary", stripFileOperationsSummary(summary))
}

func compactFileOperationCompactionEntry(
	t *testing.T,
	operations []compactionFileOperation,
) database.EntryEntity {
	t.Helper()

	entry := compactFileOperationEntry(
		"prior-compaction",
		database.EntryTypeCompaction,
		database.RoleCompactionSummary,
		compactFileOperationTestSummary,
	)
	data := entryDataForCompaction{Details: map[string]any{compactionFileOperationsKey: operations}}
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
	return compactionTestEntry(entryID, entryType, role, content)
}

func compactFileOperationPathArgsJSON() string {
	return `{"path":"` + compactFileOperationTestSourcePath + `"}`
}

func assertCompactionFileOperation(
	t *testing.T,
	operations []compactionFileOperation,
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
