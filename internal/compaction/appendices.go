package compaction

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	activeDetailsKindKey   = "kind"
	activeKindAgent        = "agent"
	activeKindAgentTask    = "agent_task"
	activeKindToolAlias    = "tool"
	activeKindTool         = "background_tool"
	activeKindWorkflow     = "workflow"
	activeDetailsStateKey  = "state"
	activeDetailsTaskIDKey = "task_id"
	activeStateQueued      = "queued"
	activeStateRunning     = "running"
	activeStateUnknown     = "unknown"
	// validationCanceledBritish accepts the British spelling from providers and tools.
	validationCanceledBritish = "cancel" + "led"

	// MaxValidationRecords bounds validation records retained in a checkpoint.
	MaxValidationRecords = 32
	// MaxActiveWorkRecords bounds active work records retained in a checkpoint.
	MaxActiveWorkRecords = 32
	maxRecordFieldBytes  = 4096
	validationTokenLimit = 1024
	activeWorkTokenLimit = 768
	fileTokenLimit       = 1024
)

// ValidationOutcome describes the durable result of a validation command.
type ValidationOutcome string

const (
	// ValidationPassed indicates a successful validation command.
	ValidationPassed ValidationOutcome = "passed"
	// ValidationFailed indicates an unsuccessful validation command.
	ValidationFailed ValidationOutcome = "failed"
	// ValidationCanceled indicates a canceled validation command.
	ValidationCanceled ValidationOutcome = "canceled"
	// ValidationUnknown indicates a validation command without a recognized result.
	ValidationUnknown ValidationOutcome = "unknown"
)

// ValidationRecord associates an exact validation command with its durable outcome.
type ValidationRecord struct {
	EntryID string            `json:"entry_id"`
	Command string            `json:"command"`
	Outcome ValidationOutcome `json:"outcome"`
}

// ActiveWorkRecord describes a durable task that may still require attention.
type ActiveWorkRecord struct {
	EntryID       string `json:"entry_id"`
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	OwningSession string `json:"owning_session,omitempty"`
}

// CollectValidationRecords extracts exact bash commands and durable outcomes,
// retaining the latest outcome for each exact command.
func CollectValidationRecords(entries []database.EntryEntity) []ValidationRecord {
	byCommand := map[string]ValidationRecord{}
	order := []string{}

	for index := range entries {
		entry := &entries[index]

		records := validationRecordsFromCompaction(entry)
		if command, ok := validationCommand(entry); ok {
			records = append(records, ValidationRecord{
				EntryID: entry.ID, Command: command, Outcome: validationOutcome(entry.ToolStatus),
			})
		}

		for _, record := range records {
			record.EntryID = boundedRecordField(record.EntryID)
			record.Command = boundedRecordField(record.Command)

			if strings.TrimSpace(record.Command) == "" {
				continue
			}

			if _, exists := byCommand[record.Command]; exists {
				order = removeString(order, record.Command)
			}

			byCommand[record.Command] = record
			order = append(order, record.Command)
		}
	}

	if len(order) > MaxValidationRecords {
		order = order[len(order)-MaxValidationRecords:]
	}

	out := make([]ValidationRecord, 0, len(order))
	for _, command := range order {
		out = append(out, byCommand[command])
	}

	return out
}

func validationRecordsFromCompaction(entry *database.EntryEntity) []ValidationRecord {
	if entry.Type != database.EntryTypeCompaction {
		return nil
	}

	var data struct {
		Details struct {
			Records []ValidationRecord `json:"validation_records"`
		} `json:"details"`
	}
	if json.Unmarshal([]byte(entry.DataJSON), &data) != nil {
		return nil
	}

	return data.Details.Records
}

func validationCommand(entry *database.EntryEntity) (string, bool) {
	if entry.ToolName != string(tool.NameBash) {
		return "", false
	}

	var args map[string]any
	if json.Unmarshal([]byte(entry.ToolArgsJSON), &args) != nil {
		return "", false
	}

	command, ok := stringArgument(args, "command")
	if !ok || !isValidationCommand(command) {
		return "", false
	}

	return command, true
}

func isValidationCommand(command string) bool {
	command = strings.TrimSpace(command)
	for _, prefix := range []string{
		"go test ", "go vet ", "golangci-lint ", "staticcheck ",
		"task build", "task ci", "task test", "task lint", "task fmt-check",
		"mise exec -- go test ", "mise exec -- go vet ", "mise exec -- golangci-lint ",
		"mise exec -- staticcheck ", "mise exec -- task build", "mise exec -- task ci",
		"mise exec -- task test", "mise exec -- task lint", "mise exec -- task fmt-check",
	} {
		if command == strings.TrimSpace(prefix) || strings.HasPrefix(command, prefix) {
			return true
		}
	}

	return false
}

func validationOutcome(status string) ValidationOutcome {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "success", "passed":
		return ValidationPassed
	case "failed", "error":
		return ValidationFailed
	case "canceled", validationCanceledBritish:
		return ValidationCanceled
	default:
		return ValidationUnknown
	}
}

func removeString(values []string, target string) []string {
	for index, value := range values {
		if value == target {
			return append(values[:index], values[index+1:]...)
		}
	}

	return values
}

// CollectActiveWorkRecords reads bounded typed task references from durable details.
func CollectActiveWorkRecords(entries []database.EntryEntity) []ActiveWorkRecord {
	byKey := map[string]ActiveWorkRecord{}
	order := []string{}

	for index := range entries {
		entry := &entries[index]

		records := activeWorkRecordsFromCompaction(entry)
		if record, ok := activeWorkRecord(entry); ok {
			records = append(records, record)
		}

		for index := range records {
			addActiveWorkRecord(byKey, &order, &records[index])
		}
	}

	out := make([]ActiveWorkRecord, 0, len(order))
	for _, key := range order {
		record := byKey[key]
		if isActiveWorkState(record.State) {
			out = append(out, record)
		}
	}

	if len(out) > MaxActiveWorkRecords {
		out = out[len(out)-MaxActiveWorkRecords:]
	}

	return out
}

func addActiveWorkRecord(
	byKey map[string]ActiveWorkRecord,
	order *[]string,
	record *ActiveWorkRecord,
) {
	boundedActiveWorkRecord(record)

	if record.Kind == "" || record.ID == "" {
		return
	}

	key := record.Kind + "\x00" + record.ID
	if _, exists := byKey[key]; !exists {
		*order = append(*order, key)
	}

	byKey[key] = *record
}

func activeWorkRecordsFromCompaction(entry *database.EntryEntity) []ActiveWorkRecord {
	if entry.Type != database.EntryTypeCompaction {
		return nil
	}

	var data struct {
		Details struct {
			Records []ActiveWorkRecord `json:"active_work_records"`
		} `json:"details"`
	}
	if json.Unmarshal([]byte(entry.DataJSON), &data) != nil {
		return nil
	}

	return data.Details.Records
}

func activeWorkRecord(entry *database.EntryEntity) (ActiveWorkRecord, bool) {
	var data struct {
		Details map[string]any `json:"details"`
	}
	if json.Unmarshal([]byte(entry.DataJSON), &data) != nil {
		return emptyActiveWorkRecord(), false
	}

	kind, _ := stringArgument(data.Details, activeDetailsKindKey)
	kind = normalizedActiveWorkKind(kind)

	workID := activeWorkID(data.Details, kind)

	if !isActiveWorkKind(kind) || workID == "" {
		return emptyActiveWorkRecord(), false
	}

	state, _ := stringArgument(data.Details, activeDetailsStateKey)
	if state == "" {
		state = activeStateUnknown
	}

	owner, _ := stringArgument(data.Details, "owning_session")
	if owner == "" {
		owner = entry.SessionID
	}

	return ActiveWorkRecord{
		EntryID:       entry.ID,
		ID:            workID,
		Kind:          kind,
		State:         state,
		OwningSession: owner,
	}, true
}

func emptyActiveWorkRecord() ActiveWorkRecord {
	return ActiveWorkRecord{EntryID: "", ID: "", Kind: "", State: "", OwningSession: ""}
}

func activeWorkID(details map[string]any, kind string) string {
	workID, _ := stringArgument(details, activeDetailsTaskIDKey)
	if workID != "" || kind != activeKindWorkflow {
		return workID
	}

	for _, key := range []string{"workflow_task_id", "run_id", "workflow_id"} {
		if workID, _ = stringArgument(details, key); workID != "" {
			return workID
		}
	}

	return ""
}

func normalizedActiveWorkKind(kind string) string {
	switch kind {
	case activeKindAgent, activeKindAgentTask:
		return activeKindAgentTask
	case activeKindToolAlias, activeKindTool:
		return activeKindTool
	case activeKindWorkflow:
		return activeKindWorkflow
	default:
		return ""
	}
}

func isActiveWorkKind(kind string) bool {
	return normalizedActiveWorkKind(kind) != ""
}

func isActiveWorkState(state string) bool {
	return state == activeStateQueued || state == activeStateRunning || state == activeStateUnknown
}

func boundedActiveWorkRecord(record *ActiveWorkRecord) {
	record.EntryID = boundedRecordField(record.EntryID)
	record.ID = boundedRecordField(record.ID)
	record.Kind = boundedRecordField(record.Kind)
	record.State = boundedRecordField(record.State)
	record.OwningSession = boundedRecordField(record.OwningSession)
}

func boundedRecordField(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxRecordFieldBytes {
		return value
	}

	return strings.ToValidUTF8(value[:maxRecordFieldBytes], "")
}

// AppendDeterministicState is the sole appendix writer.
func AppendDeterministicState(
	summary string,
	files []FileOperation,
	validations []ValidationRecord,
	work []ActiveWorkRecord,
) string {
	summary = StripDeterministicState(summary)
	sections := []string{summary}
	files = retainNewest(files, maxFileOperations)
	validations = retainNewest(validations, MaxValidationRecords)
	work = retainNewest(work, MaxActiveWorkRecords)
	fileLines := make([]string, 0, len(files))

	for _, record := range files {
		fileLines = append(fileLines, "- "+record.Action+": "+record.Path+optionalVia(record.Tool))
	}

	validationLines := make([]string, 0, len(validations))
	for _, record := range validations {
		line := fmt.Sprintf("- %s: %s (entry %s)", record.Outcome, record.Command, record.EntryID)
		validationLines = append(validationLines, line)
	}

	workLines := make([]string, 0, len(work))
	for _, record := range work {
		line := fmt.Sprintf("- %s %s: %s", record.Kind, record.ID, record.State)
		if record.OwningSession != "" {
			line += " (session " + record.OwningSession + ")"
		}

		workLines = append(workLines, line)
	}

	sections = appendBoundedSection(sections, "### Librecode file operations", fileLines, fileTokenLimit)
	sections = appendBoundedSection(sections, "### Librecode validation records", validationLines, validationTokenLimit)
	sections = appendBoundedSection(sections, "### Librecode active work records", workLines, activeWorkTokenLimit)

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func retainNewest[T any](records []T, limit int) []T {
	if len(records) <= limit {
		return records
	}

	return records[len(records)-limit:]
}

func optionalVia(name string) string {
	if name == "" {
		return ""
	}

	return " (via " + name + ")"
}

func appendBoundedSection(sections []string, heading string, lines []string, limit int) []string {
	if len(lines) == 0 {
		return sections
	}

	kept := []string{}
	used := contextwindow.EstimateTokens(heading)
	omitted := 0

	for index, line := range slices.Backward(lines) {
		cost := contextwindow.EstimateTokens(line)
		if used+cost > limit {
			omitted = index + 1

			break
		}

		kept = append([]string{line}, kept...)
		used += cost
	}

	if omitted > 0 {
		kept = appendOmissionMarker(kept, used, omitted, limit)
	}

	return append(sections, heading+"\n"+strings.Join(kept, "\n"))
}

func appendOmissionMarker(kept []string, used, omitted, limit int) []string {
	marker := fmt.Sprintf("- ... %d older records omitted", omitted)

	for len(kept) > 0 && used+contextwindow.EstimateTokens(marker) > limit {
		used -= contextwindow.EstimateTokens(kept[0])
		kept = kept[1:]
		omitted++
		marker = fmt.Sprintf("- ... %d older records omitted", omitted)
	}

	if used+contextwindow.EstimateTokens(marker) > limit {
		return kept
	}

	return append(kept, marker)
}

// StripDeterministicState removes deterministic appendices from a generated summary.
func StripDeterministicState(summary string) string {
	headings := []string{
		"### Librecode file operations",
		"### Librecode validation records",
		"### Librecode active work records",
		fileOperationsHeader,
	}

	for _, heading := range headings {
		if before, _, ok := strings.Cut(summary, heading); ok {
			summary = before
		}
	}

	return strings.TrimSpace(summary)
}
