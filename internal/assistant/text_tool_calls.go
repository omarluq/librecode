package assistant

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
)

const (
	textToolNameField     = "tool_name"
	textToolOldTextKey    = "old_text"
	textToolNewTextKey    = "new_text"
	textToolFilePathField = "file_path"
)

var (
	textToolUsePattern = regexp.MustCompile(`(?is)<tool_use\b[^>]*>(.*?)</tool_use>`)
	textToolTagPattern = regexp.MustCompile(`(?is)<([a-zA-Z][a-zA-Z0-9_-]*)\b[^>]*>(.*?)</[a-zA-Z][a-zA-Z0-9_-]*>`)
)

func textToolCallsFromText(text string) []toolCall {
	matches := textToolUsePattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	calls := make([]toolCall, 0, len(matches))
	for index, match := range matches {
		fields := textToolFields(match[1])
		name := normalizeTextToolName(firstTextToolField(fields, textToolNameField, jsonToolNameKey, jsonToolRole))
		if name == "" {
			continue
		}
		arguments := textToolArguments(name, fields)
		argumentsJSON := encodeToolArguments(arguments)
		calls = append(calls, toolCall{
			Arguments:     arguments,
			ID:            textToolCallID(index),
			Name:          name,
			ArgumentsJSON: argumentsJSON,
			TextFallback:  true,
		})
	}

	return calls
}

func textToolFields(content string) map[string]string {
	fields := map[string]string{}
	for _, match := range textToolTagPattern.FindAllStringSubmatch(content, -1) {
		key := normalizeTextToolKey(match[1])
		if key == "" || key == anthropicToolUseType {
			continue
		}
		fields[key] = strings.TrimSpace(html.UnescapeString(match[2]))
	}

	return fields
}

func normalizeTextToolName(name string) string {
	normalized := normalizeTextToolKey(name)
	switch normalized {
	case jsonReadToolName:
		return jsonReadToolName
	case jsonBashToolName, "shell", "sh", jsonCommandKey:
		return jsonBashToolName
	case jsonEditToolName, "replace":
		return jsonEditToolName
	case jsonWriteToolName, "create":
		return jsonWriteToolName
	case jsonGrepToolName, "search":
		return jsonGrepToolName
	case "find":
		return "find"
	case "ls", "list", "list_dir", "list_directory":
		return "ls"
	default:
		return ""
	}
}

func normalizeTextToolKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")

	return value
}

func textToolArguments(name string, fields map[string]string) map[string]any {
	arguments := map[string]any{}
	for key, value := range fields {
		if key == textToolNameField || key == jsonToolNameKey || key == jsonToolRole {
			continue
		}
		arguments[textToolArgumentName(name, key)] = value
	}

	return arguments
}

func textToolArgumentName(toolName, fieldName string) string {
	switch fieldName {
	case textToolFilePathField, "filepath", "file", "filename":
		return "path"
	case textToolOldTextKey:
		return jsonOldTextKey
	case textToolNewTextKey:
		return jsonNewTextKey
	case "allow_ignored":
		return "allowIgnored"
	case "ignore_case":
		return "ignoreCase"
	default:
		if toolName == jsonBashToolName && fieldName == "cmd" {
			return jsonCommandKey
		}
		return fieldName
	}
}

func firstTextToolField(fields map[string]string, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(fields[normalizeTextToolKey(name)]); value != "" {
			return value
		}
	}

	return ""
}

func encodeToolArguments(arguments map[string]any) string {
	if len(arguments) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "{}"
	}

	return string(encoded)
}

func textToolCallID(index int) string {
	return "text_tool_call_" + strconv.Itoa(index+1)
}

func hasTextFallbackToolCalls(calls []toolCall) bool {
	for _, call := range calls {
		if call.TextFallback {
			return true
		}
	}

	return false
}

func textToolResultPrompt(events []ToolEvent) string {
	parts := make([]string, 0, len(events))
	for _, event := range events {
		label := "Tool result for " + event.Name
		body := strings.TrimSpace(event.Result)
		if event.Error != "" {
			body = strings.TrimSpace(event.Error)
		}
		if body == "" {
			body = "(tool returned no text output)"
		}
		parts = append(parts, label+":\n"+body)
	}

	return strings.Join(parts, "\n\n")
}
