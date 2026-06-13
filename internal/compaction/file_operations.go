package compaction

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"

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

var shellPathTokenPattern = regexp.MustCompile(`^[./A-Za-z0-9_@~][A-Za-z0-9_@~./+%:=,-]*$`)

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
	lower := strings.ToLower(command)
	if strings.Contains(lower, ">") {
		return true
	}

	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return false
	}

	mutatingCommands := []string{"cp", "mv", "rm", "mkdir", "touch", "tee"}
	if slices.Contains(mutatingCommands, fields[0]) {
		return true
	}

	return commandUsesInPlaceEdit(fields)
}

func commandUsesInPlaceEdit(fields []string) bool {
	if len(fields) == 0 || (fields[0] != "sed" && fields[0] != "perl") {
		return false
	}

	return slices.ContainsFunc(fields[1:], func(field string) bool {
		return field == "-i" || strings.HasPrefix(field, "-i.") || strings.HasPrefix(field, "-i'") ||
			strings.HasPrefix(field, "-i\"")
	})
}

func shellCommandPathTokens(command string) []string {
	fields := strings.Fields(command)
	paths := []string{}

	for index, field := range fields {
		if index == 0 || shellTokenIsIgnored(field) {
			continue
		}

		path := cleanShellPathToken(field)
		if !looksLikeShellPath(path) || slices.Contains(paths, path) {
			continue
		}

		paths = append(paths, path)
	}

	return paths
}

func shellTokenIsIgnored(token string) bool {
	trimmed := strings.TrimSpace(token)

	return trimmed == "" || strings.HasPrefix(trimmed, "-") || trimmed == "|" || trimmed == "&&" ||
		trimmed == "||" || trimmed == ";" || trimmed == ">" || trimmed == ">>" || trimmed == "<"
}

func cleanShellPathToken(token string) string {
	trimmed := strings.Trim(token, "'\"`(){}[],:;")
	if strings.HasPrefix(trimmed, "s/") {
		return ""
	}

	return trimmed
}

func looksLikeShellPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "-") || !shellPathTokenPattern.MatchString(path) {
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
