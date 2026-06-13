package database

import (
	"encoding/json"
	"fmt"
)

func (data *EntryDataEntity) applyLegacyFields(content []byte) {
	fields := decodeObjectFields(content)
	legacyBoolPointer(fields, "modelFacing", &data.ModelFacing)
	legacyString(fields, "fromId", &data.FromID)
	legacyString(fields, "branchFromEntryId", &data.BranchFromEntryID)
	legacyString(fields, "thinkingLevel", &data.ThinkingLevel)
	legacyString(fields, "toolName", &data.ToolName)
	legacyString(fields, "toolStatus", &data.ToolStatus)
	legacyString(fields, "toolArgsJson", &data.ToolArgsJSON)
	legacyString(fields, "firstKeptEntryId", &data.FirstKeptEntryID)
	legacyString(fields, "compactionFirstKeptEntryId", &data.CompactionFirstKeptEntryID)
	legacyInt(fields, "tokenEstimate", &data.TokenEstimate)
	legacyInt(fields, "compactionTokensBefore", &data.CompactionTokensBefore)
	legacyInt(fields, "tokensBefore", &data.TokensBefore)
	legacyBool(fields, "fromHook", &data.FromHook)
}

func legacyUsage(content []byte, usage *EntryTokenUsageEntity) {
	fields := decodeObjectFields(content)
	legacyInt(fields, "contextWindow", &usage.ContextWindow)
	legacyInt(fields, "contextTokens", &usage.ContextTokens)
	legacyInt(fields, "inputTokens", &usage.InputTokens)
	legacyInt(fields, "outputTokens", &usage.OutputTokens)
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

func legacyString(fields map[string]json.RawMessage, key string, target *string) {
	if *target != "" {
		return
	}

	var value string
	if err := json.Unmarshal(fields[key], &value); err == nil {
		*target = value
	}
}

func legacyInt(fields map[string]json.RawMessage, key string, target *int) {
	if *target != 0 {
		return
	}

	var value int
	if err := json.Unmarshal(fields[key], &value); err == nil {
		*target = value
	}
}

func legacyBool(fields map[string]json.RawMessage, key string, target *bool) {
	if *target {
		return
	}

	var value bool
	if err := json.Unmarshal(fields[key], &value); err == nil {
		*target = value
	}
}

func legacyBoolPointer(fields map[string]json.RawMessage, key string, target **bool) {
	if *target != nil {
		return
	}

	var value bool
	if err := json.Unmarshal(fields[key], &value); err == nil {
		*target = &value
	}
}
