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
	Content *string `json:"content"`
	Path    string  `json:"path"`
}

// WriteTool creates or overwrites complete files.
type WriteTool struct {
	locks *fileMutationLocks
	cwd   string
}

// NewWriteTool creates the write tool for cwd.
func NewWriteTool(cwd string) *WriteTool {
	return newWriteTool(cwd, newFileMutationLocks())
}

func newWriteTool(cwd string, locks *fileMutationLocks) *WriteTool {
	return &WriteTool{locks: locks, cwd: cwd}
}

// Definition returns write tool metadata.
func (writeTool *WriteTool) Definition() Definition {
	return Definition{
		Schema:        inputSchemaForName(NameWrite),
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
func (writeTool *WriteTool) Execute(ctx context.Context, input Arguments) (Result, error) {
	prepared, err := writeTool.prepareExecution(input)
	if err != nil {
		return emptyToolResult(), err
	}

	return writeTool.locks.mutate(ctx, prepared.mutationPath, func() (Result, error) {
		return prepared.execute(ctx)
	})
}

func (writeTool *WriteTool) prepareExecution(input Arguments) (preparedExecution, error) {
	var args WriteInput
	if err := decodeInput(input, &args); err != nil {
		return preparedExecution{}, err
	}

	absolutePath, err := writeTool.resolveInput(args)
	if err != nil {
		return preparedExecution{}, err
	}

	return preparedExecution{
		execute: func(ctx context.Context) (Result, error) {
			return writeTool.writeLocked(ctx, absolutePath, args)
		},
		mutationPath:  absolutePath,
		mutationLocks: writeTool.locks,
	}, nil
}

// Write creates or overwrites one file.
func (writeTool *WriteTool) Write(ctx context.Context, input WriteInput) (Result, error) {
	absolutePath, err := writeTool.resolveInput(input)
	if err != nil {
		return emptyToolResult(), err
	}

	return writeTool.locks.mutate(ctx, absolutePath, func() (Result, error) {
		return writeTool.writeLocked(ctx, absolutePath, input)
	})
}

func (writeTool *WriteTool) resolveInput(input WriteInput) (string, error) {
	if strings.TrimSpace(input.Path) == "" {
		return "", oops.In("tool").Code("write_path_required").Errorf("write path is required")
	}

	if input.Content == nil {
		return "", oops.In("tool").Code("write_content_required").Errorf("write content is required")
	}

	absolutePath, err := ResolveToCWD(input.Path, writeTool.cwd)
	if err != nil {
		return "", oops.In("tool").Code("write_resolve_path").Wrapf(err, "resolve write path")
	}

	return absolutePath, nil
}

func (*WriteTool) writeLocked(ctx context.Context, absolutePath string, input WriteInput) (Result, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return emptyToolResult(), ctxErr
	}

	if err := os.MkdirAll(filepath.Dir(absolutePath), privateDirMode); err != nil {
		return emptyToolResult(), oops.In("tool").Code("write_create_parent").Wrapf(err, "create parent directory")
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return emptyToolResult(), ctxErr
	}

	content := *input.Content
	if err := os.WriteFile(absolutePath, []byte(content), privateFileMode); err != nil {
		return emptyToolResult(), oops.
			In("tool").
			Code("write_file").
			With("path", input.Path).
			Wrapf(err, "write file")
	}

	return TextResult(
		fmt.Sprintf("Successfully wrote %s to %s", FormatSize(len(content)), input.Path),
		map[string]any{},
	), nil
}
