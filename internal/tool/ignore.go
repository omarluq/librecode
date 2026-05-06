package tool

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const gitignoreFileName = ".gitignore"

var defaultReadIgnorePatterns = []string{
	".git/",
	"node_modules/",
	".env",
	".gocache/",
	".gomodcache/",
	".tmp/",
	"bin/",
	"skills/",
}

type ignoreRule struct {
	pattern       string
	source        string
	anchored      bool
	directoryOnly bool
	negated       bool
}

func ignoredReadPath(absolutePath, cwd string) (ignored bool, reason string) {
	workspaceRoot, err := workspaceRoot(cwd)
	if err != nil {
		return false, ""
	}
	targetPath, err := filepath.Abs(absolutePath)
	if err != nil {
		return false, ""
	}
	relativePath, err := filepath.Rel(workspaceRoot, targetPath)
	if err != nil || pathEscapesRoot(relativePath) {
		return false, ""
	}
	relativePath = filepath.ToSlash(relativePath)
	if relativePath == "." || relativePath == "" {
		return false, ""
	}

	ignored = false
	reason = ""
	for _, rule := range readIgnoreRules(workspaceRoot) {
		if !rule.matches(relativePath) {
			continue
		}
		ignored = !rule.negated
		reason = rule.source
	}

	return ignored, reason
}

func workspaceRoot(cwd string) (string, error) {
	root := cwd
	if root == "" {
		root = "."
	}
	resolvedRoot, err := ResolveToCWD(root, "")
	if err != nil {
		return "", err
	}

	return filepath.Abs(resolvedRoot)
}

func pathEscapesRoot(relativePath string) bool {
	return relativePath == ".." || strings.HasPrefix(relativePath, "../") || filepath.IsAbs(relativePath)
}

func readIgnoreRules(workspaceRoot string) []ignoreRule {
	patterns := append([]string{}, defaultReadIgnorePatterns...)
	patterns = append(patterns, workspaceGitignorePatterns(workspaceRoot)...)
	rules := make([]ignoreRule, 0, len(patterns))
	for _, pattern := range patterns {
		rule, ok := parseIgnoreRule(pattern)
		if ok {
			rules = append(rules, rule)
		}
	}

	return rules
}

func workspaceGitignorePatterns(workspaceRoot string) []string {
	content, err := fs.ReadFile(os.DirFS(workspaceRoot), gitignoreFileName)
	if err != nil {
		return nil
	}

	return strings.Split(string(content), "\n")
}

func parseIgnoreRule(line string) (ignoreRule, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return emptyIgnoreRule(), false
	}
	negated := false
	if strings.HasPrefix(trimmed, "!") {
		negated = true
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
	}
	if trimmed == "" {
		return emptyIgnoreRule(), false
	}
	directoryOnly := strings.HasSuffix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	anchored := strings.HasPrefix(trimmed, "/")
	trimmed = strings.TrimPrefix(trimmed, "/")
	trimmed = path.Clean(filepath.ToSlash(trimmed))
	if trimmed == "." || trimmed == "" {
		return emptyIgnoreRule(), false
	}

	return ignoreRule{
		pattern:       trimmed,
		source:        line,
		anchored:      anchored,
		directoryOnly: directoryOnly,
		negated:       negated,
	}, true
}

func emptyIgnoreRule() ignoreRule {
	return ignoreRule{pattern: "", source: "", anchored: false, directoryOnly: false, negated: false}
}

func (rule ignoreRule) matches(relativePath string) bool {
	if rule.directoryOnly {
		return rule.matchesDirectory(relativePath)
	}
	if rule.anchored || strings.Contains(rule.pattern, "/") {
		return matchIgnoreGlob(rule.pattern, relativePath)
	}

	return matchIgnoreGlob(rule.pattern, path.Base(relativePath))
}

func (rule ignoreRule) matchesDirectory(relativePath string) bool {
	for _, directory := range ancestorDirectories(relativePath) {
		if rule.anchored || strings.Contains(rule.pattern, "/") {
			if matchIgnoreGlob(rule.pattern, directory) {
				return true
			}
			continue
		}
		if matchIgnoreGlob(rule.pattern, path.Base(directory)) {
			return true
		}
	}

	return false
}

func ancestorDirectories(relativePath string) []string {
	parts := strings.Split(relativePath, "/")
	if len(parts) <= 1 {
		return []string{relativePath}
	}
	directories := make([]string, 0, len(parts)-1)
	for index := 1; index < len(parts); index++ {
		directories = append(directories, strings.Join(parts[:index], "/"))
	}

	return directories
}

func matchIgnoreGlob(pattern, value string) bool {
	matcher, err := newGlobMatcher(pattern)
	if err != nil {
		return false
	}

	return matcher(value)
}
