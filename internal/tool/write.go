package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/oops"
)

// WriteInput contains arguments for the write tool.
type WriteInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteTool creates or overwrites complete files.
type WriteTool struct {
	cwd string
}

// NewWriteTool creates the write tool for cwd.
func NewWriteTool(cwd string) *WriteTool {
	return &WriteTool{cwd: cwd}
}

// Definition returns write tool metadata.
func (writeTool *WriteTool) Definition() Definition {
	return Definition{
		Name:          NameWrite,
		Label:         "write",
		Description:   "Write content to a file. Creates parent directories and overwrites existing files.",
		PromptSnippet: "Create or overwrite files",
		PromptGuidelines: []string{
			"Use write only for new files or complete rewrites.",
		},
		ReadOnly: false,
	}
}

// Execute runs the write tool.
func (writeTool *WriteTool) Execute(ctx context.Context, input map[string]any) (Result, error) {
	args, err := decodeInput[WriteInput](input)
	if err != nil {
		return emptyToolResult(), err
	}

	return writeTool.Write(ctx, args)
}

// Write creates or overwrites one file.
func (writeTool *WriteTool) Write(ctx context.Context, input WriteInput) (Result, error) {
	if strings.TrimSpace(input.Path) == "" {
		return emptyToolResult(), oops.In("tool").Code("write_path_required").Errorf("write path is required")
	}
	absolutePath, err := ResolveToCWD(input.Path, writeTool.cwd)
	if err != nil {
		return emptyToolResult(), oops.In("tool").Code("write_resolve_path").Wrapf(err, "resolve write path")
	}

	return withFileMutation(absolutePath, func() (Result, error) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return emptyToolResult(), ctxErr
		}
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o750); err != nil {
			return emptyToolResult(), oops.In("tool").Code("write_create_parent").Wrapf(err, "create parent directory")
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return emptyToolResult(), ctxErr
		}

		if err := os.WriteFile(absolutePath, []byte(input.Content), 0o600); err != nil {
			return emptyToolResult(), oops.
				In("tool").
				Code("write_file").
				With("path", input.Path).
				Wrapf(err, "write file")
		}

		return TextResult(
			fmt.Sprintf("Successfully wrote %d bytes to %s", len([]byte(input.Content)), input.Path),
			map[string]any{},
		), nil
	})
}
