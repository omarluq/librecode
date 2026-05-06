package tool

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

const defaultFindLimit = 1000

var ignoredSearchDirs = []string{".git", "node_modules"}

// FindInput contains arguments for the find tool.
type FindInput struct {
	Limit   *int   `json:"limit,omitempty"`
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

// FindTool searches file paths by glob pattern.
type FindTool struct {
	cwd string
}

// NewFindTool creates the find tool for cwd.
func NewFindTool(cwd string) *FindTool {
	return &FindTool{cwd: cwd}
}

// Definition returns find tool metadata.
func (findTool *FindTool) Definition() Definition {
	return Definition{
		Name:          NameFind,
		Label:         "find",
		Description:   findDescription(),
		PromptSnippet: "Find files by glob pattern",
		PromptGuidelines: []string{
			"Use find to discover files before reading or editing them.",
		},
		ReadOnly: true,
	}
}

// Execute runs the find tool.
func (findTool *FindTool) Execute(ctx context.Context, input map[string]any) (Result, error) {
	args, err := decodeInput[FindInput](input)
	if err != nil {
		return Result{Content: []ContentBlock{}, Details: map[string]any{}}, err
	}

	return findTool.Find(ctx, args)
}

// Find searches for file paths that match the requested glob pattern.
func (findTool *FindTool) Find(ctx context.Context, input FindInput) (Result, error) {
	if strings.TrimSpace(input.Pattern) == "" {
		return emptyToolResult(), oops.In("tool").Code("find_pattern_required").Errorf("find pattern is required")
	}
	limit, err := positiveLimit(input.Limit, defaultFindLimit, "find")
	if err != nil {
		return emptyToolResult(), oops.In("tool").Code("find_invalid_limit").Wrapf(err, "validate find limit")
	}
	searchRoot, err := findTool.searchRoot(input.Path)
	if err != nil {
		return emptyToolResult(), err
	}

	results, err := collectFindResults(ctx, searchRoot, input.Pattern, limit)
	if err != nil {
		return emptyToolResult(), err
	}
	if len(results) == 0 {
		return TextResult("No files found matching pattern", map[string]any{}), nil
	}

	return formatFindResults(results, limit), nil
}

func findDescription() string {
	return fmt.Sprintf(
		"Search for files by glob pattern. Returns matching file paths relative to the search directory. "+
			"Skips .git and node_modules. Output is truncated to %d results or %s.",
		defaultFindLimit,
		FormatSize(DefaultMaxBytes),
	)
}

func (findTool *FindTool) searchRoot(searchPath string) (string, error) {
	if searchPath == "" {
		searchPath = "."
	}
	absolutePath, err := ResolveToCWD(searchPath, findTool.cwd)
	if err != nil {
		return "", oops.In("tool").Code("find_resolve_path").Wrapf(err, "resolve find path")
	}
	info, err := filepathStat(absolutePath)
	if err != nil {
		return "", oops.
			In("tool").
			Code("find_path_not_found").
			With("path", absolutePath).
			Wrapf(err, "stat find path")
	}
	if !info.IsDir() {
		return "", oops.
			In("tool").
			Code("find_not_directory").
			With("path", absolutePath).
			Errorf("find path is not a directory")
	}

	return absolutePath, nil
}

func collectFindResults(ctx context.Context, searchRoot, pattern string, limit int) ([]string, error) {
	matcher, err := newGlobMatcher(pattern)
	if err != nil {
		return []string{}, oops.In("tool").Code("find_invalid_pattern").Wrapf(err, "compile find pattern")
	}

	state := &findWalkState{
		ctx:        ctx,
		matcher:    matcher,
		results:    []string{},
		searchRoot: searchRoot,
		limit:      limit,
	}
	if walkErr := filepath.WalkDir(searchRoot, state.visit); walkErr != nil {
		return []string{}, oops.In("tool").Code("find_walk").Wrapf(walkErr, "walk find path")
	}
	sort.Strings(state.results)

	return state.results, nil
}

type findWalkState struct {
	ctx        context.Context
	matcher    globMatcher
	searchRoot string
	results    []string
	limit      int
}

func (state *findWalkState) visit(currentPath string, dirEntry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if err := state.ctx.Err(); err != nil {
		return err
	}
	if shouldSkipSearchEntry(dirEntry) {
		return filepath.SkipDir
	}
	if dirEntry.IsDir() {
		return nil
	}
	if len(state.results) >= state.limit {
		return filepath.SkipAll
	}

	return state.addMatch(currentPath)
}

func (state *findWalkState) addMatch(currentPath string) error {
	relativePath, err := filepath.Rel(state.searchRoot, currentPath)
	if err != nil {
		return err
	}
	relativePath = filepath.ToSlash(relativePath)
	if state.matcher(relativePath) {
		state.results = append(state.results, relativePath)
	}

	return nil
}

func shouldSkipSearchEntry(dirEntry fs.DirEntry) bool {
	return dirEntry.IsDir() && lo.Contains(ignoredSearchDirs, dirEntry.Name())
}

func formatFindResults(results []string, limit int) Result {
	truncation := TruncateHead(strings.Join(results, "\n"), TruncationOptions{MaxLines: 1 << 30, MaxBytes: 0})
	resultLimitReached := len(results) >= limit
	output := truncation.Content
	details := map[string]any{}
	notices := findNotices(limit, resultLimitReached, &truncation)
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}
	if resultLimitReached {
		details["resultLimitReached"] = limit
	}
	if truncation.Truncated {
		details[detailTruncation] = truncation
	}

	return TextResult(output, details)
}

func findNotices(limit int, resultLimitReached bool, truncation *TruncationResult) []string {
	notices := []string{}
	if resultLimitReached {
		notices = append(
			notices,
			fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", limit, limit*2),
		)
	}
	if truncation.Truncated {
		notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
	}

	return notices
}

type globMatcher func(relativePath string) bool

func newGlobMatcher(pattern string) (globMatcher, error) {
	normalizedPattern := path.Clean(filepath.ToSlash(pattern))
	if normalizedPattern == "." {
		normalizedPattern = pattern
	}
	if !strings.Contains(normalizedPattern, "**") {
		return simpleGlobMatcher(normalizedPattern), nil
	}

	compiled, err := regexp.Compile("^" + globToRegex(normalizedPattern) + "$")
	if err != nil {
		return nil, err
	}

	return compiled.MatchString, nil
}

func simpleGlobMatcher(pattern string) globMatcher {
	matchPath := strings.Contains(pattern, "/")
	return func(relativePath string) bool {
		candidate := relativePath
		if !matchPath {
			candidate = path.Base(relativePath)
		}
		matched, err := path.Match(pattern, candidate)
		return err == nil && matched
	}
}

func globToRegex(pattern string) string {
	var builder strings.Builder
	for index := 0; index < len(pattern); index++ {
		character := pattern[index]
		switch character {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				builder.WriteString(".*")
				index++
				continue
			}
			builder.WriteString("[^/]*")
		case '?':
			builder.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			builder.WriteByte('\\')
			builder.WriteByte(character)
		default:
			builder.WriteByte(character)
		}
	}

	return builder.String()
}
