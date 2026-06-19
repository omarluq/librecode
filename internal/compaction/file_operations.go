package compaction

import (
	"encoding/json"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	// FileOperationsKey is the details key used for preserved file-operation metadata.
	FileOperationsKey    = "file_operations"
	fileOperationsHeader = "File operations preserved during compaction:"
	fileActionRead       = "read"
	fileActionModified   = "modified"
	maxFileOperations    = 64
)

// FileOperation records file activity that should survive compaction summaries.
type FileOperation struct {
	EntryID string `json:"entry_id,omitempty"`
	Action  string `json:"action"`
	Path    string `json:"path"`
	Tool    string `json:"tool,omitempty"`
	Command string `json:"command,omitempty"`
}

// CollectFileOperations extracts bounded, de-duplicated file operations from compacted entries.
func CollectFileOperations(entries []database.EntryEntity) []FileOperation {
	operations := []FileOperation{}
	seen := map[string]struct{}{}

	for index := range entries {
		entry := &entries[index]
		for _, operation := range fileOperationsFromPriorCompaction(entry) {
			operations = appendUniqueFileOperation(operations, seen, &operation)
		}

		for _, operation := range fileOperationsFromToolEntry(entry) {
			operations = appendUniqueFileOperation(operations, seen, &operation)
		}

		if len(operations) >= maxFileOperations {
			return operations[:maxFileOperations]
		}
	}

	return operations
}

func appendUniqueFileOperation(
	operations []FileOperation,
	seen map[string]struct{},
	operation *FileOperation,
) []FileOperation {
	if operation == nil {
		return operations
	}

	operation.Action = strings.TrimSpace(operation.Action)
	operation.Path = strings.TrimSpace(operation.Path)
	operation.Tool = strings.TrimSpace(operation.Tool)

	operation.Command = strings.TrimSpace(operation.Command)
	if operation.Action == "" || operation.Path == "" {
		return operations
	}

	key := operation.Action + "\x00" + operation.Path + "\x00" + operation.Tool + "\x00" + operation.Command
	if _, ok := seen[key]; ok {
		return operations
	}

	seen[key] = struct{}{}

	return append(operations, *operation)
}

func fileOperationsFromPriorCompaction(entry *database.EntryEntity) []FileOperation {
	if entry.Type != database.EntryTypeCompaction {
		return nil
	}

	data := entryData{Details: nil}
	if err := json.Unmarshal([]byte(entry.DataJSON), &data); err != nil {
		return nil
	}

	rawOperations, ok := data.Details[FileOperationsKey]
	if !ok {
		return nil
	}

	encoded, err := json.Marshal(rawOperations)
	if err != nil {
		return nil
	}

	operations := []FileOperation{}
	if err := json.Unmarshal(encoded, &operations); err != nil {
		return nil
	}

	return operations
}

func fileOperationsFromToolEntry(entry *database.EntryEntity) []FileOperation {
	if entry.Type != database.EntryTypeMessage || entry.Message.Role != database.RoleToolResult {
		return nil
	}

	args := map[string]any{}
	if strings.TrimSpace(entry.ToolArgsJSON) != "" {
		if err := json.Unmarshal([]byte(entry.ToolArgsJSON), &args); err != nil {
			return nil
		}
	}

	switch tool.Name(entry.ToolName) {
	case tool.NameRead:
		return pathArgumentFileOperation(entry, args, fileActionRead)
	case tool.NameEdit, tool.NameWrite:
		return pathArgumentFileOperation(entry, args, fileActionModified)
	case tool.NameBash:
		return bashFileOperations(entry, args)
	case tool.NameFind, tool.NameGrep, tool.NameLS, tool.NameAST:
		return pathArgumentFileOperation(entry, args, fileActionRead)
	}

	return nil
}

func pathArgumentFileOperation(
	entry *database.EntryEntity,
	args map[string]any,
	action string,
) []FileOperation {
	path, ok := stringArgument(args, "path")
	if !ok {
		path, ok = stringArgument(args, "pattern")
	}

	if !ok {
		return nil
	}

	return []FileOperation{{
		EntryID: entry.ID,
		Action:  action,
		Path:    path,
		Tool:    entry.ToolName,
		Command: "",
	}}
}

func bashFileOperations(entry *database.EntryEntity, args map[string]any) []FileOperation {
	command, ok := stringArgument(args, "command")
	if !ok {
		return nil
	}

	paths := shellCommandPathTokens(command)
	if len(paths) == 0 {
		return nil
	}

	action := fileActionRead
	if shellCommandLooksMutating(command) {
		action = fileActionModified
	}

	operations := make([]FileOperation, 0, len(paths))
	for _, path := range paths {
		operations = append(operations, FileOperation{
			EntryID: entry.ID,
			Action:  action,
			Path:    path,
			Tool:    entry.ToolName,
			Command: truncateCommand(command),
		})
	}

	return operations
}

func stringArgument(args map[string]any, key string) (string, bool) {
	value, ok := args[key].(string)
	if !ok {
		return "", false
	}

	value = strings.TrimSpace(value)

	return value, value != ""
}

func shellCommandLooksMutating(command string) bool {
	file, ok := parseShellCommand(command)
	if !ok {
		return false
	}

	mutating := false

	syntax.Walk(file, func(node syntax.Node) bool {
		if mutating || node == nil {
			return false
		}

		switch typedNode := node.(type) {
		case *syntax.Stmt:
			mutating = statementHasOutputRedirect(typedNode)
		case *syntax.CallExpr:
			mutating = shellCallLooksMutating(typedNode)
		}

		return !mutating
	})

	return mutating
}

func statementHasOutputRedirect(statement *syntax.Stmt) bool {
	return slices.ContainsFunc(statement.Redirs, func(redirect *syntax.Redirect) bool {
		return redirect != nil && shellRedirectWrites(redirect.Op)
	})
}

func shellRedirectWrites(operator syntax.RedirOperator) bool {
	switch operator {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.RdrClob, syntax.AppClob,
		syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob:
		return true
	case syntax.RdrIn, syntax.DplIn, syntax.DplOut, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
		return false
	}

	return false
}

func shellCallLooksMutating(call *syntax.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}

	command, ok := singleShellStaticField(call.Args[0])
	if !ok {
		return false
	}

	command = strings.ToLower(command)

	if shellCommandMutates(command) {
		return true
	}

	return commandUsesInPlaceEdit(command, call.Args[1:])
}

func shellCommandMutates(command string) bool {
	switch command {
	case "cp", "mv", "rm", "mkdir", "touch", "tee":
		return true
	default:
		return false
	}
}

func commandUsesInPlaceEdit(command string, args []*syntax.Word) bool {
	if command != "sed" && command != "perl" {
		return false
	}

	return slices.ContainsFunc(args, func(arg *syntax.Word) bool {
		values, ok := shellStaticFields(arg)
		if !ok {
			return false
		}

		return slices.ContainsFunc(values, func(value string) bool {
			return value == "-i" || strings.HasPrefix(value, "-i.")
		})
	})
}

func shellCommandPathTokens(command string) []string {
	file, ok := parseShellCommand(command)
	if !ok {
		return []string{}
	}

	paths := []string{}

	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil {
			return false
		}

		switch typedNode := node.(type) {
		case *syntax.CallExpr:
			paths = appendShellCallPaths(paths, typedNode)
		case *syntax.Stmt:
			paths = appendShellRedirectPaths(paths, typedNode)
		}

		return true
	})

	return paths
}

func parseShellCommand(command string) (*syntax.File, bool) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(command), "")

	return file, err == nil
}

func appendShellCallPaths(paths []string, call *syntax.CallExpr) []string {
	for index, arg := range call.Args {
		if index == 0 {
			continue
		}

		for _, path := range shellPathFields(arg) {
			if slices.Contains(paths, path) {
				continue
			}

			paths = append(paths, path)
		}
	}

	return paths
}

func appendShellRedirectPaths(paths []string, statement *syntax.Stmt) []string {
	for _, redirect := range statement.Redirs {
		if redirect == nil || redirect.Word == nil {
			continue
		}

		for _, path := range shellPathFields(redirect.Word) {
			if slices.Contains(paths, path) {
				continue
			}

			paths = append(paths, path)
		}
	}

	return paths
}

func singleShellStaticField(word *syntax.Word) (string, bool) {
	fields, ok := shellStaticFields(word)
	if !ok || len(fields) != 1 {
		return "", false
	}

	return fields[0], true
}

func shellPathFields(word *syntax.Word) []string {
	fields, ok := shellStaticFields(word)
	if !ok {
		return nil
	}

	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		if looksLikeShellPath(field) {
			paths = append(paths, field)
		}
	}

	return paths
}

func shellStaticFields(word *syntax.Word) ([]string, bool) {
	if word == nil || len(word.Parts) == 0 {
		return nil, false
	}

	fields, err := expand.Fields(&expand.Config{NoUnset: true}, word)
	if err != nil {
		return nil, false
	}

	return fields, true
}

func looksLikeShellPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || strings.HasPrefix(path, "-") || strings.HasPrefix(path, "s/") {
		return false
	}

	return strings.Contains(path, "/") || strings.Contains(path, ".")
}

const (
	commandPreviewLimit       = 160
	commandPreviewSuffixWidth = len("...")
)

func truncateCommand(command string) string {
	command = strings.TrimSpace(command)
	if len(command) <= commandPreviewLimit {
		return command
	}

	return command[:commandPreviewLimit-commandPreviewSuffixWidth] + "..."
}

// AppendFileOperationsSummary replaces stale operation text and appends current operations.
func AppendFileOperationsSummary(summary string, operations []FileOperation) string {
	summary = StripFileOperationsSummary(summary)
	if len(operations) == 0 {
		return summary
	}

	lines := []string{strings.TrimSpace(summary), "", fileOperationsHeader}

	for _, operation := range operations[:min(len(operations), maxFileOperations)] {
		line := "- " + operation.Action + ": " + operation.Path
		if operation.Tool != "" {
			line += " (via " + operation.Tool + ")"
		}

		lines = append(lines, line)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// StripFileOperationsSummary removes the generated file-operation section from summary text.
func StripFileOperationsSummary(summary string) string {
	before, _, ok := strings.Cut(summary, fileOperationsHeader)
	if !ok {
		return strings.TrimSpace(summary)
	}

	return strings.TrimSpace(before)
}

type entryData struct {
	Details map[string]any `json:"details,omitempty"`
}
