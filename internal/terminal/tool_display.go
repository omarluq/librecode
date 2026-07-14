package terminal

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	toolDisplayDefaultPath = "."
	toolDisplayMaxArgs     = 3
	toolDisplayGrepOptions = 3
)

type toolSummaryRenderer func(map[string]any, string) string

type toolDisplayStatus int

const (
	toolDisplayPending toolDisplayStatus = iota
	toolDisplaySuccess
	toolDisplayError
)

type toolDisplay struct {
	Title         string
	Name          string
	ArgumentsJSON string
	DetailsJSON   string
	Output        string
	Error         string
	Status        toolDisplayStatus
}

func toolDisplayFromCall(call assistant.ToolCallEvent) toolDisplay {
	return toolDisplay{
		Title:         toolSummary(call.Name, call.ArgumentsJSON, toolArgumentsMap(call.Arguments)),
		Name:          call.Name,
		ArgumentsJSON: call.ArgumentsJSON,
		DetailsJSON:   "",
		Output:        "",
		Error:         "",
		Status:        toolDisplayPending,
	}
}

func toolDisplayFromParsedEvent(event *parsedToolEvent) toolDisplay {
	status := toolDisplaySuccess
	if event.Error != "" {
		status = toolDisplayError
	}

	return toolDisplay{
		Title:         toolSummary(event.Name, event.ArgumentsJSON, nil),
		Name:          event.Name,
		ArgumentsJSON: event.ArgumentsJSON,
		DetailsJSON:   event.DetailsJSON,
		Output:        event.Output,
		Error:         event.Error,
		Status:        status,
	}
}

func toolSummary(name, argumentsJSON string, arguments map[string]any) string {
	trimmedName := strings.TrimSpace(name)
	if after, matched := strings.CutPrefix(trimmedName, "load skill: "); matched {
		return "loaded skill " + strings.TrimSpace(after)
	}

	args := arguments
	if args == nil {
		args = decodeToolArgs(argumentsJSON)
	}

	render, matched := toolSummaryRendererFor(trimmedName)
	if !matched {
		return unknownToolSummary(trimmedName, args, argumentsJSON)
	}

	return render(args, trimmedName)
}

func toolSummaryRendererFor(name string) (toolSummaryRenderer, bool) {
	toolName := tool.Name(name)
	if render, matched := fileToolSummaryRenderer(toolName); matched {
		return render, true
	}

	return otherToolSummaryRenderer(toolName)
}

func fileToolSummaryRenderer(name tool.Name) (toolSummaryRenderer, bool) {
	switch string(name) {
	case string(tool.NameRead):
		return readToolSummary, true
	case string(tool.NameEdit):
		return editToolSummary, true
	case string(tool.NameWrite):
		return writeToolSummary, true
	case string(tool.NameGrep):
		return grepToolSummary, true
	case string(tool.NameFind):
		return findToolSummary, true
	default:
		return nil, false
	}
}

func otherToolSummaryRenderer(name tool.Name) (toolSummaryRenderer, bool) {
	switch string(name) {
	case string(tool.NameBash):
		return bashToolSummary, true
	case string(tool.NameLS):
		return lsToolSummary, true
	case string(tool.NameAST):
		return astToolSummary, true
	case string(tool.NameFetch):
		return fetchToolSummary, true
	default:
		return nil, false
	}
}

func toolArgumentsMap(arguments tool.Arguments) map[string]any {
	return decodeToolArgs(arguments.String())
}

func decodeToolArgs(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil
	}

	return args
}

func bashToolSummary(args map[string]any, fallback string) string {
	command := stringArg(args, "command")
	if command == "" {
		return fallbackToolName(fallback)
	}

	title := "$ " + command
	if timeout, matched := numberArg(args, "timeout"); matched {
		title += " (timeout " + formatSeconds(timeout) + ")"
	}

	return title
}

func readToolSummary(args map[string]any, fallback string) string {
	path := stringArg(args, "path")

	if path == "" {
		return fallbackToolName(fallback)
	}

	title := "read " + path

	if offset, hasOffset := intArg(args, "offset"); hasOffset {
		if limit, hasLimit := intArg(args, "limit"); hasLimit && limit > 0 {
			title += fmt.Sprintf(":%d-%d", offset, offset+limit-1)
		} else {
			title += fmt.Sprintf(":%d", offset)
		}
	}

	return title
}

func editToolSummary(args map[string]any, fallback string) string {
	title := pathToolSummary("edit", args, fallback)
	if count := arrayLenArg(args, "edits"); count > 1 {
		title += " (" + tui.Int(count) + " edits)"
	}

	return title
}

func writeToolSummary(args map[string]any, fallback string) string {
	return pathToolSummary("write", args, fallback)
}

func grepToolSummary(args map[string]any, fallback string) string {
	pattern := stringArg(args, "pattern")
	path := stringArg(args, "path")

	if path == "" {
		path = toolDisplayDefaultPath
	}

	if pattern == "" {
		return fallbackToolName(fallback)
	}

	title := "grep " + strconv.Quote(pattern) + " in " + path
	suffixes := make([]string, 0, toolDisplayGrepOptions)

	if glob := stringArg(args, "glob"); glob != "" {
		suffixes = append(suffixes, "glob "+glob)
	}

	if boolArg(args, "literal") {
		suffixes = append(suffixes, "literal")
	}

	if boolArg(args, "ignore_case") {
		suffixes = append(suffixes, "ignore case")
	}

	if len(suffixes) > 0 {
		title += " (" + strings.Join(suffixes, ", ") + ")"
	}

	return title
}

func findToolSummary(args map[string]any, fallback string) string {
	pattern := stringArg(args, "pattern")
	path := stringArg(args, "path")

	if path == "" {
		path = toolDisplayDefaultPath
	}

	if pattern == "" {
		return fallbackToolName(fallback)
	}

	return "find " + pattern + " under " + path
}

func lsToolSummary(args map[string]any, _ string) string {
	path := stringArg(args, "path")
	if path == "" {
		path = toolDisplayDefaultPath
	}

	return "ls " + path
}

func fetchToolSummary(args map[string]any, fallback string) string {
	url := stringArg(args, "url")
	if url == "" {
		return fallbackToolName(fallback)
	}

	format := stringArg(args, "format")
	if format == "" || format == "markdown" {
		return "fetch " + url
	}

	var summary strings.Builder
	summary.WriteString("fetch ")
	summary.WriteString(format)
	summary.WriteString(" ")
	summary.WriteString(url)

	return summary.String()
}

func astToolSummary(args map[string]any, fallback string) string {
	path := stringArg(args, "path")
	mode := stringArg(args, "mode")

	if mode == "" {
		mode = "outline"
	}

	if path == "" {
		return fallbackToolName(fallback)
	}

	title := "ast " + mode + " " + path
	if line, matched := intArg(args, "line"); matched {
		title += ":" + tui.Int(line)
	}

	if depth, matched := intArg(args, "depth"); matched {
		title += " depth " + tui.Int(depth)
	}

	if query := stringArg(args, "query"); query != "" {
		title += " query " + strconv.Quote(query)
	}

	return title
}

func pathToolSummary(verb string, args map[string]any, fallback string) string {
	path := stringArg(args, "path")

	if path == "" {
		return fallbackToolName(fallback)
	}

	return verb + " " + path
}

func unknownToolSummary(name string, args map[string]any, raw string) string {
	name = fallbackToolName(name)

	if len(args) == 0 {
		if compact := compactJSON(raw); compact != "" {
			return name + " " + compact
		}

		return name
	}

	parts := compactArgPairs(args, toolDisplayMaxArgs)
	if len(parts) == 0 {
		return name
	}

	return name + " " + strings.Join(parts, " ")
}

func compactArgPairs(args map[string]any, limit int) []string {
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+compactArgValue(args[key]))
	}

	return pairs
}

func compactArgValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed)
	case float64:
		return formatNumber(typed)
	case bool:
		return strconv.FormatBool(typed)
	case nil:
		return "null"
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}

		return string(encoded)
	}
}

func fallbackToolName(name string) string {
	if strings.TrimSpace(name) == "" {
		return toolSectionTool
	}

	return strings.TrimSpace(name)
}

func stringArg(args map[string]any, key string) string {
	value, exists := args[key]
	if !exists {
		return ""
	}

	text, matched := value.(string)
	if !matched {
		return ""
	}

	return strings.TrimSpace(text)
}

func intArg(args map[string]any, key string) (int, bool) {
	value, matched := numberArg(args, key)
	if !matched {
		return 0, false
	}

	return int(value), true
}

func numberArg(args map[string]any, key string) (float64, bool) {
	value, exists := args[key]
	if !exists {
		return 0, false
	}

	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()

		return number, err == nil
	default:
		return 0, false
	}
}

func boolArg(args map[string]any, key string) bool {
	value, exists := args[key]
	if !exists {
		return false
	}

	boolean, matched := value.(bool)

	return matched && boolean
}

func arrayLenArg(args map[string]any, key string) int {
	value, exists := args[key]
	if !exists {
		return 0
	}

	items, matched := value.([]any)
	if !matched {
		return 0
	}

	return len(items)
}

func formatSeconds(value float64) string {
	if value == float64(int(value)) {
		return tui.Int(int(value)) + "s"
	}

	return strconv.FormatFloat(value, 'f', -1, 64) + "s"
}

func formatNumber(value float64) string {
	if value == float64(int(value)) {
		return tui.Int(int(value))
	}

	return strconv.FormatFloat(value, 'f', -1, 64)
}
