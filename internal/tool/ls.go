package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samber/lo"
)

const defaultLSLimit = 500

// LSInput contains arguments for the ls tool.
type LSInput struct {
	Limit *int   `json:"limit,omitempty"`
	Path  string `json:"path,omitempty"`
}

// LSTool lists directory entries.
type LSTool struct {
	cwd string
}

// NewLSTool creates the ls tool for cwd.
func NewLSTool(cwd string) *LSTool {
	return &LSTool{cwd: cwd}
}

// Definition returns ls tool metadata.
func (lsTool *LSTool) Definition() Definition {
	return Definition{
		Name:          NameLS,
		Label:         "ls",
		Description:   lsDescription(),
		PromptSnippet: "List directory contents",
		PromptGuidelines: []string{
			"Use ls to inspect directory contents before reading files.",
		},
		ReadOnly: true,
	}
}

// Execute runs the ls tool.
func (lsTool *LSTool) Execute(ctx context.Context, input map[string]any) (Result, error) {
	args, err := decodeInput[LSInput](input)
	if err != nil {
		return emptyToolResult(), err
	}

	return lsTool.LS(ctx, args)
}

// LS lists a directory alphabetically and marks directories with a slash.
func (lsTool *LSTool) LS(ctx context.Context, input LSInput) (Result, error) {
	limit, err := positiveLimit(input.Limit, defaultLSLimit, "ls")
	if err != nil {
		return emptyToolResult(), err
	}
	searchPath := input.Path
	if searchPath == "" {
		searchPath = "."
	}
	absolutePath, err := ResolveToCWD(searchPath, lsTool.cwd)
	if err != nil {
		return emptyToolResult(), err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return emptyToolResult(), ctxErr
	}

	entries, err := readSortedDirectory(absolutePath)
	if err != nil {
		return emptyToolResult(), err
	}

	return formatLSResults(absolutePath, entries, limit), nil
}

func lsDescription() string {
	return fmt.Sprintf(
		"List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. "+
			"Includes dotfiles. Output is truncated to %d entries or %s.",
		defaultLSLimit,
		FormatSize(DefaultMaxBytes),
	)
}

func readSortedDirectory(absolutePath string) ([]os.DirEntry, error) {
	info, err := filepathStat(absolutePath)
	if err != nil {
		return []os.DirEntry{}, fmt.Errorf("path not found: %s", absolutePath)
	}
	if !info.IsDir() {
		return []os.DirEntry{}, fmt.Errorf("not a directory: %s", absolutePath)
	}

	entries, err := os.ReadDir(absolutePath)
	if err != nil {
		return []os.DirEntry{}, fmt.Errorf("cannot read directory: %w", err)
	}
	sort.Slice(entries, func(leftIndex int, rightIndex int) bool {
		leftName := strings.ToLower(entries[leftIndex].Name())
		rightName := strings.ToLower(entries[rightIndex].Name())
		return leftName < rightName
	})

	return entries, nil
}

func formatLSResults(absolutePath string, entries []os.DirEntry, limit int) Result {
	limitedEntries := entries[:min(len(entries), limit)]
	results := lo.FilterMap(limitedEntries, func(entry os.DirEntry, _ int) (string, bool) {
		return directoryEntryDisplayName(absolutePath, entry)
	})
	if len(results) == 0 {
		return TextResult("(empty directory)", map[string]any{})
	}

	entryLimitReached := len(entries) > limit
	truncation := TruncateHead(strings.Join(results, "\n"), TruncationOptions{MaxLines: 1 << 30, MaxBytes: 0})
	output := truncation.Content
	details := map[string]any{}
	notices := lsNotices(limit, entryLimitReached, &truncation)
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}
	if entryLimitReached {
		details["entryLimitReached"] = limit
	}
	if truncation.Truncated {
		details[detailTruncation] = truncation
	}

	return TextResult(output, details)
}

func directoryEntryDisplayName(absolutePath string, entry os.DirEntry) (string, bool) {
	name := entry.Name()
	info, err := entry.Info()
	if err != nil {
		return "", false
	}
	if info.IsDir() || isDirectorySymlink(filepath.Join(absolutePath, entry.Name())) {
		name += "/"
	}

	return name, true
}

func isDirectorySymlink(absolutePath string) bool {
	info, err := filepathStat(absolutePath)
	return err == nil && info.IsDir()
}

func lsNotices(limit int, entryLimitReached bool, truncation *TruncationResult) []string {
	notices := []string{}
	if entryLimitReached {
		notices = append(notices, fmt.Sprintf("%d entries limit reached. Use limit=%d for more", limit, limit*2))
	}
	if truncation.Truncated {
		notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
	}

	return notices
}

func positiveLimit(limit *int, defaultLimit int, label string) (int, error) {
	if limit == nil {
		return defaultLimit, nil
	}
	if *limit < 1 {
		return 0, fmt.Errorf("%s limit must be greater than zero", label)
	}

	return *limit, nil
}
