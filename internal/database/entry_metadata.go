package database

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const charsPerEstimatedToken = 4

func applyEntryMetadata(entry *EntryEntity) error {
	data, err := dataFromEntry(entry)
	if err != nil {
		return err
	}

	entry.TokenEstimate = estimateEntryTokens(entry)
	if data.ModelFacing != nil {
		entry.ModelFacing = *data.ModelFacing
	} else {
		entry.ModelFacing = entryParticipatesInModelContext(entry)
	}

	entry.Display = entryDisplaysInTranscript(entry, &data)
	entry.CompactionFirstKeptEntryID = data.CompactionFirstKeptEntryID
	entry.CompactionTokensBefore = data.CompactionTokensBefore
	entry.BranchFromEntryID = data.BranchFromEntryID
	entry.ToolName = data.ToolName
	entry.ToolStatus = data.ToolStatus
	entry.ToolArgsJSON = data.ToolArgsJSON

	if entry.Message.Role == RoleToolResult {
		metadata := parseToolMetadata(entry.Message.Content)
		entry.ToolName = firstNonEmpty(entry.ToolName, metadata.Name)
		entry.ToolStatus = firstNonEmpty(entry.ToolStatus, metadata.Status)
		entry.ToolArgsJSON = firstNonEmpty(entry.ToolArgsJSON, metadata.ArgsJSON)
	}

	// Dedicated columns are the sole owner of these projections. They are
	// accepted from API metadata above, then removed before data_json is stored.
	data.ToolName = ""
	data.ToolStatus = ""
	data.ToolArgsJSON = ""
	data.TokenEstimate = 0
	data.ModelFacing = nil
	data.Display = nil
	data.CompactionFirstKeptEntryID = ""
	data.CompactionTokensBefore = 0
	data.BranchFromEntryID = ""

	dataJSON, err := dataJSONFromEntity(&data)
	if err != nil {
		return err
	}

	entry.DataJSON = normalizeDataJSON(dataJSON)

	return nil
}

func estimateEntryTokens(entry *EntryEntity) int {
	text := entry.Message.Content
	if strings.TrimSpace(text) == "" {
		text = entry.Summary
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}

	return max(1, (utf8.RuneCountInString(trimmed)+charsPerEstimatedToken-1)/charsPerEstimatedToken)
}

func entryParticipatesInModelContext(entry *EntryEntity) bool {
	switch entry.Type {
	case EntryTypeMessage:
		return entry.Message.Role == RoleUser || entry.Message.Role == RoleAssistant
	case EntryTypeCustomMessage, EntryTypeBranchSummary, EntryTypeCompaction:
		return true
	case EntryTypeCustom, EntryTypeLabel, EntryTypeModelChange, EntryTypeSessionInfo, EntryTypeThinkingLevelChange:
		return false
	}

	return false
}

func entryDisplaysInTranscript(entry *EntryEntity, data *EntryDataEntity) bool {
	if data != nil && data.Display != nil {
		return *data.Display
	}

	switch entry.Type {
	case EntryTypeMessage,
		EntryTypeCustom,
		EntryTypeCustomMessage,
		EntryTypeCompaction,
		EntryTypeBranchSummary:
		return true
	case EntryTypeModelChange,
		EntryTypeThinkingLevelChange,
		EntryTypeLabel,
		EntryTypeSessionInfo:
		return false
	}

	return true
}

type toolMetadata struct {
	Name     string
	Status   string
	ArgsJSON string
}

func parseToolMetadata(content string) toolMetadata {
	metadata := toolMetadata{Name: "", Status: "success", ArgsJSON: ""}
	sections := splitToolSections(content)
	metadata.Name = strings.TrimSpace(sections["tool"])

	metadata.ArgsJSON = strings.TrimSpace(sections["arguments"])
	if strings.TrimSpace(sections["error"]) != "" {
		metadata.Status = "error"
	}

	if metadata.ArgsJSON != "" {
		metadata.ArgsJSON = firstNonEmpty(compactJSON(metadata.ArgsJSON), metadata.ArgsJSON)
	}

	return metadata
}

func splitToolSections(content string) map[string]string {
	sections := map[string]string{}
	current := ""

	lines := strings.SplitSeq(content, "\n")
	for line := range lines {
		name, value, ok := splitToolHeader(line)
		if ok {
			current = name
			sections[current] = value

			continue
		}

		if current == "" {
			continue
		}

		if sections[current] == "" {
			sections[current] = line
		} else {
			sections[current] += "\n" + line
		}
	}

	return sections
}

func splitToolHeader(line string) (name, value string, ok bool) {
	for _, section := range []string{"tool", "arguments", "error", "details", "output"} {
		prefix := section + ":"
		if after, ok0 := strings.CutPrefix(line, prefix); ok0 {
			return section, strings.TrimSpace(after), true
		}
	}

	return "", "", false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func compactJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}

	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return value
	}

	encoded, err := json.Marshal(decoded)
	if err != nil {
		return value
	}

	return string(encoded)
}
