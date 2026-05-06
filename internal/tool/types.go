// Package tool provides Pi-style built-in coding tools for local agent turns.
package tool

import (
	"context"
	"strings"

	"github.com/samber/lo"
)

// Name identifies a built-in coding tool.
type Name string

const (
	// NameRead reads a file from disk.
	NameRead Name = "read"
	// NameBash executes a shell command.
	NameBash Name = "bash"
	// NameEdit applies exact text replacements to one file.
	NameEdit Name = "edit"
	// NameWrite writes a complete file.
	NameWrite Name = "write"
	// NameGrep searches file contents.
	NameGrep Name = "grep"
	// NameFind finds file paths by glob.
	NameFind Name = "find"
	// NameLS lists directory entries.
	NameLS Name = "ls"
)

const (
	// ContentTypeText is a plain text tool result block.
	ContentTypeText = "text"
	// ContentTypeImage is an inline base64 image result block.
	ContentTypeImage = "image"
)

// Definition describes a built-in coding tool.
type Definition struct {
	Name             Name     `json:"name"`
	Label            string   `json:"label"`
	Description      string   `json:"description"`
	PromptSnippet    string   `json:"prompt_snippet"`
	PromptGuidelines []string `json:"prompt_guidelines"`
	ReadOnly         bool     `json:"read_only"`
}

// ContentBlock is one model-facing piece of tool output.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
}

// Result is returned by every built-in tool.
type Result struct {
	Details map[string]any `json:"details"`
	Content []ContentBlock `json:"content"`
}

// Executor runs a built-in tool with decoded input.
type Executor interface {
	Definition() Definition
	Execute(ctx context.Context, input map[string]any) (Result, error)
}

// TextResult creates a text-only tool result.
func TextResult(text string, details map[string]any) Result {
	if details == nil {
		details = map[string]any{}
	}

	return Result{
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: text, Data: "", MIMEType: ""},
		},
		Details: details,
	}
}

// Text joins all text blocks in a tool result.
func (result Result) Text() string {
	texts := lo.FilterMap(result.Content, func(block ContentBlock, _ int) (string, bool) {
		return block.Text, block.Type == ContentTypeText && block.Text != ""
	})

	return strings.Join(texts, "\n")
}
