package assistant

import (
	"encoding/json"
	"fmt"
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
	textToolUsePattern     = regexp.MustCompile(`(?is)<tool_use\b[^>]*>(.*?)</tool_use>`)
	textToolOpeningPattern = regexp.MustCompile(`(?is)<\s*([a-zA-Z][a-zA-Z0-9_-]*)\b[^>]*>`)
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
	searchOffset := 0
	lowerContent := asciiLower(content)
	for searchOffset < len(content) {
		match := textToolOpeningPattern.FindStringSubmatchIndex(content[searchOffset:])
		if match == nil {
			break
		}
		openStart := searchOffset + match[0]
		openEnd := searchOffset + match[1]
		tag := content[searchOffset+match[2] : searchOffset+match[3]]
		key := normalizeTextToolKey(tag)
		if key == "" || key == anthropicToolUseType {
			searchOffset = openEnd
			continue
		}
		closeStart, closeEnd, ok := findTextToolClosingTag(lowerContent, tag, openEnd)
		if !ok {
			searchOffset = openEnd
			continue
		}
		value := html.UnescapeString(content[openEnd:closeStart])
		if isTextToolContainerKey(key) {
			mergeTextToolFields(fields, textToolFields(value))
			mergeTextToolFields(fields, textToolJSONFields(value))
		} else {
			fields[key] = value
		}
		searchOffset = max(closeEnd, openStart+1)
	}

	return fields
}

func findTextToolClosingTag(lowerContent, tag string, after int) (closeStart, closeEnd int, ok bool) {
	closingTag := "</" + asciiLower(tag) + ">"
	closeStart = strings.Index(lowerContent[after:], closingTag)
	if closeStart == -1 {
		return 0, 0, false
	}
	closeStart += after

	return closeStart, closeStart + len(closingTag), true
}

func mergeTextToolFields(destination, source map[string]string) {
	for key, value := range source {
		if _, exists := destination[key]; !exists {
			destination[key] = value
		}
	}
}

func textToolJSONFields(value string) map[string]string {
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &payload); err != nil {
		return nil
	}
	fields := make(map[string]string, len(payload))
	for key, fieldValue := range payload {
		fields[normalizeTextToolKey(key)] = textToolJSONFieldValue(fieldValue)
	}

	return fields
}

func textToolJSONFieldValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func isTextToolContainerKey(key string) bool {
	switch key {
	case "args", "arguments", jsonInputKey, "parameters", "params":
		return true
	default:
		return false
	}
}

func asciiLower(value string) string {
	return strings.ToLower(value)
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
	case jsonFindToolName:
		return jsonFindToolName
	case jsonLSToolName, "list", "list_dir", "list_directory":
		return jsonLSToolName
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
		arguments[textToolArgumentName(name, key)] = strings.TrimSpace(value)
	}
	applyTextToolAliases(name, fields, arguments)

	return arguments
}

func applyTextToolAliases(name string, fields map[string]string, arguments map[string]any) {
	switch name {
	case jsonWriteToolName:
		content, hasContent := firstRawTextToolField(
			fields,
			jsonContentKey,
			"contents",
			"text",
			"body",
			"file_content",
			"file_contents",
			"new_content",
			"new_contents",
			"new_file_content",
			"new_file_contents",
			"code",
		)
		applyTextToolAlias(arguments, jsonContentKey, content, hasContent)
	case jsonEditToolName:
		oldText, hasOldText := firstRawTextToolField(fields, textToolOldTextKey, "old-text", "old")
		newText, hasNewText := firstRawTextToolField(fields, textToolNewTextKey, "new-text", "new")
		applyTextToolAlias(arguments, jsonOldTextKey, oldText, hasOldText)
		applyTextToolAlias(arguments, jsonNewTextKey, newText, hasNewText)
	case jsonBashToolName:
		command, hasCommand := firstRawTextToolField(fields, jsonCommandKey, "cmd")
		applyTextToolAlias(arguments, jsonCommandKey, command, hasCommand)
	}
}

func applyTextToolAlias(arguments map[string]any, key, value string, exists bool) {
	if !exists {
		return
	}
	arguments[key] = value
}

func textToolArgumentName(toolName, fieldName string) string {
	switch fieldName {
	case textToolFilePathField, "filepath", "file", "filename":
		return jsonPathKey
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

func firstRawTextToolField(fields map[string]string, names ...string) (string, bool) {
	for _, name := range names {
		value, ok := fields[normalizeTextToolKey(name)]
		if ok {
			return value, true
		}
	}

	return "", false
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
