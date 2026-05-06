package tool

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

const defaultGrepLimit = 100

// GrepInput contains arguments for the grep tool.
type GrepInput struct {
	Context    *int   `json:"context,omitempty"`
	Limit      *int   `json:"limit,omitempty"`
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	IgnoreCase bool   `json:"ignoreCase,omitempty"`
	Literal    bool   `json:"literal,omitempty"`
}

// GrepTool searches text files for patterns.
type GrepTool struct {
	cwd string
}

type grepTarget struct {
	absolutePath string
	displayPath  string
}

type grepSearch struct {
	matcher      grepMatcher
	targets      []grepTarget
	contextLines int
	limit        int
}

type grepMatchResult struct {
	outputLines       []string
	matchLimitReached bool
	linesTruncated    bool
}

type grepMatcher func(line string) bool

// NewGrepTool creates the grep tool for cwd.
func NewGrepTool(cwd string) *GrepTool {
	return &GrepTool{cwd: cwd}
}

// Definition returns grep tool metadata.
func (grepTool *GrepTool) Definition() Definition {
	return Definition{
		Name:          NameGrep,
		Label:         "grep",
		Description:   grepDescription(),
		PromptSnippet: "Search file contents for patterns",
		PromptGuidelines: []string{
			"Use grep to search file contents before reading large files.",
		},
		ReadOnly: true,
	}
}

// Execute runs the grep tool.
func (grepTool *GrepTool) Execute(ctx context.Context, input map[string]any) (Result, error) {
	args, err := decodeInput[GrepInput](input)
	if err != nil {
		return emptyToolResult(), err
	}

	return grepTool.Grep(ctx, args)
}

// Grep searches text files for matching lines.
func (grepTool *GrepTool) Grep(ctx context.Context, input GrepInput) (Result, error) {
	search, err := grepTool.prepareSearch(input)
	if err != nil {
		return emptyToolResult(), err
	}
	matchResult, err := runGrepSearch(ctx, search)
	if err != nil {
		return emptyToolResult(), err
	}
	if len(matchResult.outputLines) == 0 {
		return TextResult("No matches found", map[string]any{}), nil
	}

	return formatGrepResults(matchResult, search.limit), nil
}

func grepDescription() string {
	return fmt.Sprintf(
		"Search file contents for a pattern. Returns matching lines with file paths and line numbers. "+
			"Skips .git and node_modules. Output is truncated to %d matches or %s. "+
			"Long lines are truncated to %d chars.",
		defaultGrepLimit,
		FormatSize(DefaultMaxBytes),
		GrepMaxLineLength,
	)
}

func (grepTool *GrepTool) prepareSearch(input GrepInput) (grepSearch, error) {
	if strings.TrimSpace(input.Pattern) == "" {
		return grepSearch{matcher: nil, targets: []grepTarget{}, contextLines: 0, limit: 0},
			oops.In("tool").Code("grep_pattern_required").Errorf("grep pattern is required")
	}
	limit, err := positiveLimit(input.Limit, defaultGrepLimit, "grep")
	if err != nil {
		return grepSearch{matcher: nil, targets: []grepTarget{}, contextLines: 0, limit: 0},
			oops.In("tool").Code("grep_invalid_limit").Wrapf(err, "validate grep limit")
	}
	contextLines := 0
	if input.Context != nil {
		if *input.Context < 0 {
			return grepSearch{matcher: nil, targets: []grepTarget{}, contextLines: 0, limit: 0},
				oops.In("tool").Code("grep_invalid_context").Errorf("grep context cannot be negative")
		}
		contextLines = *input.Context
	}
	matcher, err := newGrepMatcher(input.Pattern, input.IgnoreCase, input.Literal)
	if err != nil {
		return grepSearch{matcher: nil, targets: []grepTarget{}, contextLines: 0, limit: 0},
			oops.In("tool").Code("grep_invalid_pattern").Wrapf(err, "compile grep pattern")
	}
	targets, err := grepTool.targets(input.Path, input.Glob)
	if err != nil {
		return grepSearch{matcher: nil, targets: []grepTarget{}, contextLines: 0, limit: 0}, err
	}

	return grepSearch{matcher: matcher, targets: targets, contextLines: contextLines, limit: limit}, nil
}

func newGrepMatcher(pattern string, ignoreCase, literal bool) (grepMatcher, error) {
	if literal {
		needle := pattern
		if ignoreCase {
			needle = strings.ToLower(needle)
		}
		return func(line string) bool {
			candidate := line
			if ignoreCase {
				candidate = strings.ToLower(candidate)
			}

			return strings.Contains(candidate, needle)
		}, nil
	}

	regexPattern := pattern
	if ignoreCase {
		regexPattern = "(?i)" + regexPattern
	}
	compiled, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil, err
	}

	return compiled.MatchString, nil
}

func (grepTool *GrepTool) targets(searchPath, glob string) ([]grepTarget, error) {
	if searchPath == "" {
		searchPath = "."
	}
	absolutePath, err := ResolveToCWD(searchPath, grepTool.cwd)
	if err != nil {
		return []grepTarget{}, oops.In("tool").Code("grep_resolve_path").Wrapf(err, "resolve grep path")
	}
	info, err := filepathStat(absolutePath)
	if err != nil {
		return []grepTarget{}, oops.
			In("tool").
			Code("grep_path_not_found").
			With("path", absolutePath).
			Wrapf(err, "stat grep path")
	}
	if !info.IsDir() {
		return []grepTarget{{absolutePath: absolutePath, displayPath: filepath.Base(absolutePath)}}, nil
	}

	return grepDirectoryTargets(absolutePath, glob)
}

func grepDirectoryTargets(root, glob string) ([]grepTarget, error) {
	matcher, err := optionalGlobMatcher(glob)
	if err != nil {
		return []grepTarget{}, oops.In("tool").Code("grep_invalid_glob").Wrapf(err, "compile grep glob")
	}

	targets := []grepTarget{}
	walkErr := filepath.WalkDir(root, func(currentPath string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if shouldSkipSearchEntry(dirEntry) {
			return filepath.SkipDir
		}
		if dirEntry.IsDir() {
			return nil
		}
		relativePath, err := filepath.Rel(root, currentPath)
		if err != nil {
			return err
		}
		displayPath := filepath.ToSlash(relativePath)
		if matcher(displayPath) {
			targets = append(targets, grepTarget{absolutePath: currentPath, displayPath: displayPath})
		}

		return nil
	})
	if walkErr != nil {
		return []grepTarget{}, oops.In("tool").Code("grep_walk").Wrapf(walkErr, "walk grep path")
	}

	return targets, nil
}

func optionalGlobMatcher(glob string) (globMatcher, error) {
	if glob == "" {
		return func(string) bool { return true }, nil
	}

	return newGlobMatcher(glob)
}

func runGrepSearch(ctx context.Context, search grepSearch) (grepMatchResult, error) {
	result := grepMatchResult{outputLines: []string{}, matchLimitReached: false, linesTruncated: false}
	matchCount := 0
	for _, target := range search.targets {
		if err := ctx.Err(); err != nil {
			return grepMatchResult{outputLines: []string{}, matchLimitReached: false, linesTruncated: false}, err
		}
		fileResult, err := grepFile(target, search.matcher, search.contextLines, search.limit-matchCount)
		if err != nil {
			continue
		}
		result.outputLines = append(result.outputLines, fileResult.outputLines...)
		result.linesTruncated = result.linesTruncated || fileResult.linesTruncated
		matchCount += fileResult.matchCount
		if matchCount >= search.limit {
			result.matchLimitReached = true
			break
		}
	}

	return result, nil
}

type grepFileResult struct {
	outputLines    []string
	matchCount     int
	linesTruncated bool
}

func grepFile(target grepTarget, matcher grepMatcher, contextLines, remainingLimit int) (grepFileResult, error) {
	if remainingLimit <= 0 {
		return grepFileResult{outputLines: []string{}, matchCount: 0, linesTruncated: false}, nil
	}

	data, err := os.ReadFile(target.absolutePath)
	if err != nil {
		return grepFileResult{outputLines: []string{}, matchCount: 0, linesTruncated: false}, err
	}
	content := string(data)
	if strings.ContainsRune(content, '\x00') {
		return grepFileResult{outputLines: []string{}, matchCount: 0, linesTruncated: false}, nil
	}

	lines := strings.Split(normalizeToLF(content), "\n")
	outputLines := []string{}
	matchCount := 0
	linesTruncated := false
	for lineIndex, line := range lines {
		if matchCount >= remainingLimit {
			break
		}
		if !matcher(line) {
			continue
		}
		blockLines, blockTruncated := formatGrepBlock(target.displayPath, lines, lineIndex, contextLines)
		outputLines = append(outputLines, blockLines...)
		linesTruncated = linesTruncated || blockTruncated
		matchCount++
	}

	return grepFileResult{outputLines: outputLines, matchCount: matchCount, linesTruncated: linesTruncated}, nil
}

func formatGrepBlock(displayPath string, lines []string, matchIndex, contextLines int) ([]string, bool) {
	startLine := matchIndex
	endLine := matchIndex
	if contextLines > 0 {
		startLine = max(0, matchIndex-contextLines)
		endLine = min(len(lines)-1, matchIndex+contextLines)
	}

	lineIndexes := lo.RangeFrom(startLine, endLine-startLine+1)
	linesTruncated := false
	outputLines := lo.Map(lineIndexes, func(lineIndex int, _ int) string {
		lineText, wasTruncated := TruncateLine(strings.TrimRight(lines[lineIndex], "\r"), GrepMaxLineLength)
		linesTruncated = linesTruncated || wasTruncated
		separator := ":"
		if lineIndex != matchIndex {
			separator = "-"
		}

		return fmt.Sprintf("%s%s%d%s %s", displayPath, separator, lineIndex+1, separator, lineText)
	})

	return outputLines, linesTruncated
}

func formatGrepResults(matchResult grepMatchResult, limit int) Result {
	rawOutput := strings.Join(matchResult.outputLines, "\n")
	truncation := TruncateHead(rawOutput, TruncationOptions{MaxLines: 1 << 30, MaxBytes: 0})
	output := truncation.Content
	details := map[string]any{}
	notices := grepNotices(limit, matchResult, &truncation)
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}
	if matchResult.matchLimitReached {
		details["matchLimitReached"] = limit
	}
	if truncation.Truncated {
		details[detailTruncation] = truncation
	}
	if matchResult.linesTruncated {
		details["linesTruncated"] = true
	}

	return TextResult(output, details)
}

func grepNotices(limit int, matchResult grepMatchResult, truncation *TruncationResult) []string {
	notices := []string{}
	if matchResult.matchLimitReached {
		notices = append(
			notices,
			fmt.Sprintf("%d matches limit reached. Use limit=%d for more, or refine pattern", limit, limit*2),
		)
	}
	if truncation.Truncated {
		notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
	}
	if matchResult.linesTruncated {
		notices = append(
			notices,
			fmt.Sprintf("Some lines truncated to %d chars. Use read tool to see full lines", GrepMaxLineLength),
		)
	}

	return notices
}
