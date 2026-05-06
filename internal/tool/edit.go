package tool

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/samber/oops"
)

// EditInput contains arguments for the edit tool.
type EditInput struct {
	Path  string        `json:"path"`
	Edits []Replacement `json:"edits"`
}

// EditTool applies exact text replacements to files.
type EditTool struct {
	cwd string
}

// NewEditTool creates the edit tool for cwd.
func NewEditTool(cwd string) *EditTool {
	return &EditTool{cwd: cwd}
}

// Definition returns edit tool metadata.
func (editTool *EditTool) Definition() Definition {
	return Definition{
		Name:          NameEdit,
		Label:         "edit",
		Description:   editDescription(),
		PromptSnippet: "Make precise file edits with exact text replacement",
		PromptGuidelines: []string{
			"Use edit for precise changes (edits[].oldText must match exactly).",
			"Use one edit call with multiple entries for separate locations in one file.",
			"Every oldText is matched against the original file and must not overlap.",
		},
		ReadOnly: false,
	}
}

// Execute runs the edit tool.
func (editTool *EditTool) Execute(ctx context.Context, input map[string]any) (Result, error) {
	args, err := decodeInput[EditInput](input)
	if err != nil {
		return emptyToolResult(), err
	}

	return editTool.Edit(ctx, args)
}

// Edit applies one or more replacements to one file.
func (editTool *EditTool) Edit(ctx context.Context, input EditInput) (Result, error) {
	if strings.TrimSpace(input.Path) == "" {
		return emptyToolResult(), oops.In("tool").Code("edit_path_required").Errorf("edit path is required")
	}
	absolutePath, err := ResolveToCWD(input.Path, editTool.cwd)
	if err != nil {
		return emptyToolResult(), oops.In("tool").Code("edit_resolve_path").Wrapf(err, "resolve edit path")
	}

	return withFileMutation(absolutePath, func() (Result, error) {
		return editTool.editLocked(ctx, absolutePath, input)
	})
}

func (editTool *EditTool) editLocked(ctx context.Context, absolutePath string, input EditInput) (Result, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return emptyToolResult(), ctxErr
	}
	//nolint:gosec // The edit tool intentionally reads user-selected workspace paths.
	rawData, err := os.ReadFile(absolutePath)
	if err != nil {
		return emptyToolResult(), oops.
			In("tool").
			Code("edit_read_file").
			With("path", input.Path).
			Wrapf(err, "read file")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return emptyToolResult(), ctxErr
	}

	bom, content := stripBOM(string(rawData))
	lineEnding := detectLineEnding(content)
	applied, err := applyEditsToNormalizedContent(normalizeToLF(content), input.Edits, input.Path)
	if err != nil {
		return emptyToolResult(), err
	}
	finalContent := bom + restoreLineEndings(applied.newContent, lineEnding)
	//nolint:gosec // The edit tool intentionally writes user-selected workspace paths.
	if err := os.WriteFile(absolutePath, []byte(finalContent), 0o600); err != nil {
		return emptyToolResult(), oops.
			In("tool").
			Code("edit_write_file").
			With("path", input.Path).
			Wrapf(err, "write file")
	}

	diffDetails := generateDiffString(applied.baseContent, applied.newContent)
	return TextResult(
		fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(input.Edits), input.Path),
		map[string]any{"diff": diffDetails.Diff, "firstChangedLine": diffDetails.FirstChangedLine},
	), nil
}

func editDescription() string {
	return "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, " +
		"non-overlapping region of the original file."
}
