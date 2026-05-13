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
	entry.ModelFacing = entryParticipatesInModelContext(entry)
	entry.Display = entryDisplaysInTranscript(entry, &data)
	entry.CompactionFirstKeptEntryID = firstNonEmpty(data.CompactionFirstKeptEntryID, data.FirstKeptEntryID)
	entry.CompactionTokensBefore = firstPositive(data.CompactionTokensBefore, data.TokensBefore)
	entry.BranchFromEntryID = firstNonEmpty(data.BranchFromEntryID, data.FromID)

	if entry.Message.Role == RoleToolResult {
		metadata := parseToolMetadata(entry.Message.Content)
		entry.ToolName = firstNonEmpty(data.ToolName, metadata.Name)
		entry.ToolStatus = firstNonEmpty(data.ToolStatus, metadata.Status)
		entry.ToolArgsJSON = firstNonEmpty(data.ToolArgsJSON, metadata.ArgsJSON)
	}

	data.ToolName = entry.ToolName
	data.ToolStatus = entry.ToolStatus
	data.ToolArgsJSON = entry.ToolArgsJSON
	data.TokenEstimate = entry.TokenEstimate
	data.ModelFacing = &entry.ModelFacing
	data.CompactionFirstKeptEntryID = entry.CompactionFirstKeptEntryID
	data.CompactionTokensBefore = entry.CompactionTokensBefore
	data.BranchFromEntryID = entry.BranchFromEntryID
	if data.Display == nil {
		data.Display = &entry.Display
	}

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
	lines := strings.Split(content, "\n")
	for _, line := range lines {
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
		if strings.HasPrefix(line, prefix) {
			return section, strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
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

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}

	return 0
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
