// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	compactionFileOperationsKey    = "file_operations"
	compactionFileOperationsHeader = "File operations preserved during compaction:"
	compactionFileActionModified   = "modified"
	maxCompactionFileOperations    = 64
)

var shellPathTokenPattern = regexp.MustCompile(`^[./A-Za-z0-9_@~][A-Za-z0-9_@~./+%:=,-]*$`)

type compactionFileOperation struct {
	EntryID string `json:"entry_id,omitempty"`
	Action  string `json:"action"`
	Path    string `json:"path"`
	Tool    string `json:"tool,omitempty"`
	Command string `json:"command,omitempty"`
}

func collectCompactionFileOperations(entries []database.EntryEntity) []compactionFileOperation {
	operations := []compactionFileOperation{}
	seen := map[string]struct{}{}
	for index := range entries {
		entry := &entries[index]
		for _, operation := range fileOperationsFromPriorCompaction(entry) {
			operations = appendUniqueCompactionFileOperation(operations, seen, &operation)
		}
		for _, operation := range fileOperationsFromToolEntry(entry) {
			operations = appendUniqueCompactionFileOperation(operations, seen, &operation)
		}
		if len(operations) >= maxCompactionFileOperations {
			return operations[:maxCompactionFileOperations]
		}
	}

	return operations
}

func appendUniqueCompactionFileOperation(
	operations []compactionFileOperation,
	seen map[string]struct{},
	operation *compactionFileOperation,
) []compactionFileOperation {
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

func fileOperationsFromPriorCompaction(entry *database.EntryEntity) []compactionFileOperation {
	if entry.Type != database.EntryTypeCompaction {
		return nil
	}
	data := entryDataForCompaction{Details: nil}
	if err := json.Unmarshal([]byte(entry.DataJSON), &data); err != nil {
		return nil
	}
	rawOperations, ok := data.Details[compactionFileOperationsKey]
	if !ok {
		return nil
	}
	encoded, err := json.Marshal(rawOperations)
	if err != nil {
		return nil
	}
	operations := []compactionFileOperation{}
	if err := json.Unmarshal(encoded, &operations); err != nil {
		return nil
	}

	return operations
}

func fileOperationsFromToolEntry(entry *database.EntryEntity) []compactionFileOperation {
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
		return pathArgumentFileOperation(entry, args, jsonReadToolName)
	case tool.NameEdit, tool.NameWrite:
		return pathArgumentFileOperation(entry, args, compactionFileActionModified)
	case tool.NameBash:
		return bashFileOperations(entry, args)
	case tool.NameFind, tool.NameGrep, tool.NameLS:
		return pathArgumentFileOperation(entry, args, jsonReadToolName)
	}

	return nil
}

func pathArgumentFileOperation(
	entry *database.EntryEntity,
	args map[string]any,
	action string,
) []compactionFileOperation {
	path, ok := stringArgument(args, "path")
	if !ok {
		path, ok = stringArgument(args, "pattern")
	}
	if !ok {
		return nil
	}

	return []compactionFileOperation{{
		EntryID: entry.ID,
		Action:  action,
		Path:    path,
		Tool:    entry.ToolName,
		Command: "",
	}}
}

func bashFileOperations(entry *database.EntryEntity, args map[string]any) []compactionFileOperation {
	command, ok := stringArgument(args, "command")
	if !ok {
		return nil
	}
	paths := shellCommandPathTokens(command)
	if len(paths) == 0 {
		return nil
	}
	action := jsonReadToolName
	if shellCommandLooksMutating(command) {
		action = compactionFileActionModified
	}
	operations := make([]compactionFileOperation, 0, len(paths))
	for _, path := range paths {
		operations = append(operations, compactionFileOperation{
			EntryID: entry.ID,
			Action:  action,
			Path:    path,
			Tool:    entry.ToolName,
			Command: truncateCompactionOperationCommand(command),
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

func truncateCompactionOperationCommand(command string) string {
	command = strings.TrimSpace(command)
	if len(command) <= 160 {
		return command
	}

	return command[:157] + "..."
}

func appendFileOperationsSummary(summary string, operations []compactionFileOperation) string {
	summary = stripFileOperationsSummary(summary)
	if len(operations) == 0 {
		return summary
	}

	builder := strings.Builder{}
	builder.WriteString(strings.TrimSpace(summary))
	builder.WriteString("\n\n")
	builder.WriteString(compactionFileOperationsHeader)
	limit := min(len(operations), maxCompactionFileOperations)
	for index := 0; index < limit; index++ {
		operation := operations[index]
		builder.WriteString("\n- ")
		builder.WriteString(operation.Action)
		builder.WriteString(": ")
		builder.WriteString(operation.Path)
		if operation.Tool != "" {
			builder.WriteString(" (via ")
			builder.WriteString(operation.Tool)
			builder.WriteString(")")
		}
	}

	return strings.TrimSpace(builder.String())
}

func stripFileOperationsSummary(summary string) string {
	index := strings.Index(summary, compactionFileOperationsHeader)
	if index < 0 {
		return strings.TrimSpace(summary)
	}

	return strings.TrimSpace(summary[:index])
}

type entryDataForCompaction struct {
	Details map[string]any `json:"details,omitempty"`
}
