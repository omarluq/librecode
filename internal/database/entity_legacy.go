package database

import (
	"encoding/json"
	"fmt"
)

func (data *EntryDataEntity) applyLegacyFields(content []byte) {
	fields := decodeObjectFields(content)
	legacyBoolPointer(fields, "modelFacing", "model_facing", &data.ModelFacing)
	legacyString(fields, "fromId", "from_id", &data.FromID)
	legacyString(fields, "branchFromEntryId", "branch_from_entry_id", &data.BranchFromEntryID)
	legacyString(fields, "thinkingLevel", "thinking_level", &data.ThinkingLevel)
	legacyString(fields, "toolName", "tool_name", &data.ToolName)
	legacyString(fields, "toolStatus", "tool_status", &data.ToolStatus)
	legacyString(fields, "toolArgsJson", "tool_args_json", &data.ToolArgsJSON)
	legacyString(fields, "firstKeptEntryId", "first_kept_entry_id", &data.FirstKeptEntryID)
	legacyString(
		fields,
		"compactionFirstKeptEntryId",
		"compaction_first_kept_entry_id",
		&data.CompactionFirstKeptEntryID,
	)
	legacyInt(fields, "tokenEstimate", "token_estimate", &data.TokenEstimate)
	legacyInt(fields, "compactionTokensBefore", "compaction_tokens_before", &data.CompactionTokensBefore)
	legacyInt(fields, "tokensBefore", "tokens_before", &data.TokensBefore)
	legacyBool(fields, "fromHook", "from_hook", &data.FromHook)
}

func legacyUsage(content []byte, usage *EntryTokenUsageEntity) {
	fields := decodeObjectFields(content)
	legacyInt(fields, "contextWindow", "context_window", &usage.ContextWindow)
	legacyInt(fields, "contextTokens", "context_tokens", &usage.ContextTokens)
	legacyInt(fields, "inputTokens", "input_tokens", &usage.InputTokens)
	legacyInt(fields, "outputTokens", "output_tokens", &usage.OutputTokens)
}

func decodeEntryJSON[T any](content []byte, target *T) error {
	if err := json.Unmarshal(content, target); err != nil {
		return fmt.Errorf("decode entry json: %w", err)
	}

	return nil
}

func decodeObjectFields(content []byte) map[string]json.RawMessage {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(content, &fields); err != nil {
		return nil
	}

	return fields
}

func legacyString(fields map[string]json.RawMessage, legacyKey, canonicalKey string, target *string) {
	if hasCanonicalField(fields, canonicalKey) || *target != "" {
		return
	}

	var value string
	if err := json.Unmarshal(fields[legacyKey], &value); err == nil {
		*target = value
	}
}

func legacyInt(fields map[string]json.RawMessage, legacyKey, canonicalKey string, target *int) {
	if hasCanonicalField(fields, canonicalKey) || *target != 0 {
		return
	}

	var value int
	if err := json.Unmarshal(fields[legacyKey], &value); err == nil {
		*target = value
	}
}

func legacyBool(fields map[string]json.RawMessage, legacyKey, canonicalKey string, target *bool) {
	if hasCanonicalField(fields, canonicalKey) || *target {
		return
	}

	var value bool
	if err := json.Unmarshal(fields[legacyKey], &value); err == nil {
		*target = value
	}
}

func legacyBoolPointer(fields map[string]json.RawMessage, legacyKey, canonicalKey string, target **bool) {
	if hasCanonicalField(fields, canonicalKey) || *target != nil {
		return
	}

	var value bool
	if err := json.Unmarshal(fields[legacyKey], &value); err == nil {
		*target = &value
	}
}

func hasCanonicalField(fields map[string]json.RawMessage, key string) bool {
	_, ok := fields[key]

	return ok
}
