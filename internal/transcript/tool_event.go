// Package transcript contains provider-neutral transcript formatting helpers shared by
// assistant persistence and terminal presentation.
package transcript

import (
	"fmt"
	"strings"
)

// ToolEvent captures the display/persistence fields of a completed tool call.
type ToolEvent struct {
	Name          string
	ArgumentsJSON string
	DetailsJSON   string
	Result        string
	Error         string
	IsError       bool
}

// FormatToolEventPersistence formats a tool event for durable transcript storage.
func FormatToolEventPersistence(event *ToolEvent) string {
	return formatToolEvent(event, true)
}

// FormatToolEventDisplay formats a tool event for terminal display.
func FormatToolEventDisplay(event *ToolEvent) string {
	return formatToolEvent(event, false)
}

func formatToolEvent(event *ToolEvent, includeStructuredError bool) string {
	if event == nil {
		return "tool: "
	}
	parts := []string{fmt.Sprintf("tool: %s", event.Name)}
	if strings.TrimSpace(event.ArgumentsJSON) != "" {
		parts = append(parts, "arguments:", event.ArgumentsJSON)
	}
	if event.Error != "" {
		parts = append(parts, "error:", event.Error)
	}
	if includeStructuredError && event.IsError {
		parts = append(parts, "is_error: true")
	}
	if strings.TrimSpace(event.DetailsJSON) != "" {
		parts = append(parts, "details:", event.DetailsJSON)
	}
	if strings.TrimSpace(event.Result) != "" {
		parts = append(parts, "output:", event.Result)
	}

	return strings.Join(parts, "\n")
}
