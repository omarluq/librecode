package tool

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/samber/hot"
)

const (
	gitignoreFileName       = ".gitignore"
	readIgnoreCacheCapacity = 16
)

func defaultReadIgnorePatterns() []string {
	return []string{
		".git/",
		"node_modules/",
		".env",
		".gocache/",
		".gomodcache/",
		".tmp/",
		"bin/",
		"/skills/",
	}
}

type ignorePattern struct {
	pattern gitignore.Pattern
	source  string
}

type readIgnoreCache struct {
	patterns *hot.HotCache[string, []gitignore.Pattern]
}

func newReadIgnoreCache() *readIgnoreCache {
	return &readIgnoreCache{
		patterns: hot.NewHotCache[string, []gitignore.Pattern](hot.WTinyLFU, readIgnoreCacheCapacity).
			WithLoaders(func(workspaceRoots []string) (map[string][]gitignore.Pattern, error) {
				patterns := make(map[string][]gitignore.Pattern, len(workspaceRoots))
				for _, workspaceRoot := range workspaceRoots {
					patterns[workspaceRoot] = readRepositoryIgnorePatterns(workspaceRoot)
				}

				return patterns, nil
			}).
			WithCopyOnRead(slices.Clone[[]gitignore.Pattern]).
			WithCopyOnWrite(slices.Clone[[]gitignore.Pattern]).
			Build(),
	}
}

func ignoredReadPath(absolutePath, cwd string, cache *readIgnoreCache) (ignored bool, reason string) {
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

	pathParts := strings.Split(relativePath, "/")
	isDir := targetIsDirectory(targetPath, workspaceRoot)
	patterns := readIgnorePatterns(workspaceRoot, cache)

	matcher := gitignore.NewMatcher(extractGitignorePatterns(patterns))
	if !matcher.Match(pathParts, isDir) {
		return false, ""
	}

	return true, matchingIgnoreReason(patterns, pathParts, isDir)
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

	absolute, err := filepath.Abs(resolvedRoot)

	return absolute, toolWrap(err, "resolve ignore root")
}

func pathEscapesRoot(relativePath string) bool {
	return relativePath == ".." || strings.HasPrefix(relativePath, "../") || filepath.IsAbs(relativePath)
}

func readIgnorePatterns(workspaceRoot string, cache *readIgnoreCache) []ignorePattern {
	patterns := make([]ignorePattern, 0, len(defaultReadIgnorePatterns()))
	for _, pattern := range defaultReadIgnorePatterns() {
		patterns = append(patterns, ignorePattern{
			pattern: gitignore.ParsePattern(pattern, nil),
			source:  pattern,
		})
	}

	repositoryPatterns := cache.repositoryPatterns(workspaceRoot)
	for _, pattern := range repositoryPatterns {
		patterns = append(patterns, ignorePattern{
			pattern: pattern,
			source:  gitignoreFileName,
		})
	}

	return patterns
}

func (cache *readIgnoreCache) repositoryPatterns(workspaceRoot string) []gitignore.Pattern {
	if cache == nil {
		return readRepositoryIgnorePatterns(workspaceRoot)
	}

	patterns, found, err := cache.patterns.Get(workspaceRoot)
	if err != nil || !found {
		return readRepositoryIgnorePatterns(workspaceRoot)
	}

	return patterns
}

func readRepositoryIgnorePatterns(workspaceRoot string) []gitignore.Pattern {
	repositoryPatterns, err := gitignore.ReadPatterns(osfs.New(workspaceRoot), nil)
	if err != nil {
		return nil
	}

	return repositoryPatterns
}

func extractGitignorePatterns(patterns []ignorePattern) []gitignore.Pattern {
	gitignorePatterns := make([]gitignore.Pattern, 0, len(patterns))
	for _, ignorePattern := range patterns {
		gitignorePatterns = append(gitignorePatterns, ignorePattern.pattern)
	}

	return gitignorePatterns
}

func matchingIgnoreReason(patterns []ignorePattern, pathParts []string, isDir bool) string {
	for _, ignorePattern := range slices.Backward(patterns) {
		if ignorePattern.pattern.Match(pathParts, isDir) == gitignore.Exclude {
			return ignorePattern.source
		}
	}

	return ""
}

func targetIsDirectory(targetPath, workspaceRoot string) bool {
	if !strings.HasPrefix(targetPath, workspaceRoot+string(filepath.Separator)) && targetPath != workspaceRoot {
		return false
	}

	info, err := os.Stat(targetPath)

	return err == nil && info.IsDir()
}
