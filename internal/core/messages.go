package core

import (
	"strconv"
	"strings"
	"time"
)

const (
	// CompactionSummaryPrefix wraps compacted conversation history.
	CompactionSummaryPrefix = "The conversation history before this point was compacted into the following summary:" +
		"\n\n<summary>\n"
	// CompactionSummarySuffix closes a compacted summary block.
	CompactionSummarySuffix = "\n</summary>"
	// BranchSummaryPrefix wraps summaries from abandoned branches.
	BranchSummaryPrefix = "The following is a summary of a branch that this conversation came back from:\n\n<summary>\n"
	// BranchSummarySuffix closes a branch summary block.
	BranchSummarySuffix = "</summary>"
)

// ContentPart is a model-facing text or image block.
type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

// LLMMessage is the generic message shape sent to a model.
type LLMMessage struct {
	Role      string        `json:"role"`
	Content   []ContentPart `json:"content"`
	Timestamp int64         `json:"timestamp"`
}

// BashExecutionMessage records a user-triggered shell command.
type BashExecutionMessage struct {
	ExitCode           *int   `json:"exitCode,omitempty"`
	Command            string `json:"command"`
	Output             string `json:"output"`
	FullOutputPath     string `json:"fullOutputPath,omitempty"`
	Timestamp          int64  `json:"timestamp"`
	Canceled           bool   "json:\"cancel\u006ced\""
	Truncated          bool   `json:"truncated"`
	ExcludeFromContext bool   `json:"excludeFromContext,omitempty"`
}

// CustomMessage is extension-injected context.
type CustomMessage struct {
	Details    any           `json:"details,omitempty"`
	CustomType string        `json:"customType"`
	Content    []ContentPart `json:"content"`
	Timestamp  int64         `json:"timestamp"`
	Display    bool          `json:"display"`
}

// BranchSummaryMessage is a summary for a branch that was left.
type BranchSummaryMessage struct {
	Summary   string `json:"summary"`
	FromID    string `json:"fromId"`
	Timestamp int64  `json:"timestamp"`
}

// CompactionSummaryMessage is a summary for compacted prior context.
type CompactionSummaryMessage struct {
	Summary      string `json:"summary"`
	Timestamp    int64  `json:"timestamp"`
	TokensBefore int    `json:"tokensBefore"`
}

// BashExecutionToText renders a shell execution as user-message context.
func BashExecutionToText(message BashExecutionMessage) string {
	var builder strings.Builder
	builder.WriteString("Ran `")
	builder.WriteString(message.Command)
	builder.WriteString("`\n")
	if message.Output != "" {
		builder.WriteString("```\n")
		builder.WriteString(message.Output)
		builder.WriteString("\n```")
	} else {
		builder.WriteString("(no output)")
	}
	if message.Canceled {
		builder.WriteString("\n\n(command canceled)")
	} else if message.ExitCode != nil && *message.ExitCode != 0 {
		builder.WriteString("\n\nCommand exited with code ")
		builder.WriteString(strconv.Itoa(*message.ExitCode))
	}
	if message.Truncated && message.FullOutputPath != "" {
		builder.WriteString("\n\n[Output truncated. Full output: ")
		builder.WriteString(message.FullOutputPath)
		builder.WriteString("]")
	}

	return builder.String()
}

// NewBranchSummaryMessage creates a branch summary message from an RFC3339 timestamp.
func NewBranchSummaryMessage(summary, fromID, timestamp string) BranchSummaryMessage {
	return BranchSummaryMessage{Summary: summary, FromID: fromID, Timestamp: timestampMillis(timestamp)}
}

// NewCompactionSummaryMessage creates a compaction summary message from an RFC3339 timestamp.
func NewCompactionSummaryMessage(summary string, tokensBefore int, timestamp string) CompactionSummaryMessage {
	return CompactionSummaryMessage{Summary: summary, Timestamp: timestampMillis(timestamp), TokensBefore: tokensBefore}
}

// NewCustomMessage creates an extension custom message from an RFC3339 timestamp.
func NewCustomMessage(
	customType string,
	content []ContentPart,
	display bool,
	details any,
	timestamp string,
) CustomMessage {
	return CustomMessage{
		Details:    details,
		Content:    content,
		CustomType: customType,
		Timestamp:  timestampMillis(timestamp),
		Display:    display,
	}
}

// BashExecutionToLLM converts a bash execution into model-facing context.
func BashExecutionToLLM(message BashExecutionMessage) (LLMMessage, bool) {
	if message.ExcludeFromContext {
		return LLMMessage{Content: []ContentPart{}, Role: "", Timestamp: 0}, false
	}

	return LLMMessage{
		Content:   []ContentPart{{Type: contentTypeText, Text: BashExecutionToText(message), Data: "", MIMEType: ""}},
		Role:      roleUser,
		Timestamp: message.Timestamp,
	}, true
}

// BranchSummaryToLLM converts a branch summary into model-facing context.
func BranchSummaryToLLM(message BranchSummaryMessage) LLMMessage {
	return LLMMessage{
		Content: []ContentPart{{
			Type:     contentTypeText,
			Text:     BranchSummaryPrefix + message.Summary + BranchSummarySuffix,
			Data:     "",
			MIMEType: "",
		}},
		Role:      roleUser,
		Timestamp: message.Timestamp,
	}
}

// CompactionSummaryToLLM converts a compaction summary into model-facing context.
func CompactionSummaryToLLM(message CompactionSummaryMessage) LLMMessage {
	return LLMMessage{
		Content: []ContentPart{{
			Type:     contentTypeText,
			Text:     CompactionSummaryPrefix + message.Summary + CompactionSummarySuffix,
			Data:     "",
			MIMEType: "",
		}},
		Role:      roleUser,
		Timestamp: message.Timestamp,
	}
}

func timestampMillis(value string) int64 {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}

	return parsed.UnixMilli()
}
