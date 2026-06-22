package tool

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/omarluq/librecode/internal/fswalk"
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
	patterns *hot.HotCache[string, repositoryIgnorePatterns]
}

type repositoryIgnorePatterns struct {
	patterns  []gitignore.Pattern
	signature repositoryIgnoreSignature
}

type repositoryIgnoreSignature struct {
	paths    []ignorePathState
	complete bool
}

type ignorePathState struct {
	path    string
	modTime int64
	size    int64
	exists  bool
	dir     bool
}

func newReadIgnoreCache() *readIgnoreCache {
	return &readIgnoreCache{
		patterns: hot.NewHotCache[string, repositoryIgnorePatterns](hot.WTinyLFU, readIgnoreCacheCapacity).
			WithLoaders(func(workspaceRoots []string) (map[string]repositoryIgnorePatterns, error) {
				patterns := make(map[string]repositoryIgnorePatterns, len(workspaceRoots))
				for _, workspaceRoot := range workspaceRoots {
					patterns[workspaceRoot] = readRepositoryIgnorePatterns(workspaceRoot)
				}

				return patterns, nil
			}).
			WithCopyOnRead(copyRepositoryIgnorePatterns).
			WithCopyOnWrite(copyRepositoryIgnorePatterns).
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
		return readRepositoryIgnorePatterns(workspaceRoot).patterns
	}

	patterns, found, err := cache.patterns.Get(workspaceRoot)
	if err != nil || !found {
		return readRepositoryIgnorePatterns(workspaceRoot).patterns
	}

	if !patterns.signature.isFresh() {
		patterns = readRepositoryIgnorePatterns(workspaceRoot)
		cache.patterns.Set(workspaceRoot, patterns)
	}

	return patterns.patterns
}

func readRepositoryIgnorePatterns(workspaceRoot string) repositoryIgnorePatterns {
	signature := readRepositoryIgnoreSignature(workspaceRoot)

	repositoryPatterns, err := gitignore.ReadPatterns(osfs.New(workspaceRoot), nil)
	if err != nil {
		return repositoryIgnorePatterns{signature: signature, patterns: nil}
	}

	return repositoryIgnorePatterns{signature: signature, patterns: repositoryPatterns}
}

func copyRepositoryIgnorePatterns(patterns repositoryIgnorePatterns) repositoryIgnorePatterns {
	patterns.patterns = slices.Clone(patterns.patterns)
	patterns.signature.paths = slices.Clone(patterns.signature.paths)

	return patterns
}

func readRepositoryIgnoreSignature(workspaceRoot string) repositoryIgnoreSignature {
	collector := newIgnoreSignatureCollector(workspaceRoot)
	err := fswalk.Walk(workspaceRoot, collector.visit)

	return collector.signature(err == nil)
}

type ignoreSignatureCollector struct {
	paths []ignorePathState
	lock  sync.Mutex
}

func newIgnoreSignatureCollector(workspaceRoot string) *ignoreSignatureCollector {
	return &ignoreSignatureCollector{
		paths: []ignorePathState{readIgnorePathState(filepath.Join(workspaceRoot, ".git", "info", "exclude"))},
		lock:  sync.Mutex{},
	}
}

func (collector *ignoreSignatureCollector) visit(path string, entry os.DirEntry, err error) error {
	if err != nil {
		return ignoreSignatureWalkError(entry)
	}

	if entry.IsDir() {
		return collector.visitDirectory(path, entry.Name())
	}

	collector.visitFile(path, entry.Name())

	return nil
}

func ignoreSignatureWalkError(entry os.DirEntry) error {
	if entry != nil && entry.IsDir() {
		return filepath.SkipDir
	}

	return nil
}

func (collector *ignoreSignatureCollector) visitDirectory(path, name string) error {
	collector.append(path)

	if name == ".git" {
		return filepath.SkipDir
	}

	return nil
}

func (collector *ignoreSignatureCollector) visitFile(path, name string) {
	if name == gitignoreFileName {
		collector.append(path)
	}
}

func (collector *ignoreSignatureCollector) append(path string) {
	collector.lock.Lock()
	defer collector.lock.Unlock()

	collector.paths = append(collector.paths, readIgnorePathState(path))
}

func (collector *ignoreSignatureCollector) signature(complete bool) repositoryIgnoreSignature {
	collector.lock.Lock()
	defer collector.lock.Unlock()

	return repositoryIgnoreSignature{
		paths:    slices.Clone(collector.paths),
		complete: complete,
	}
}

func readIgnorePathState(path string) ignorePathState {
	cleanPath := filepath.Clean(path)

	info, err := os.Stat(cleanPath)
	if err != nil {
		return ignorePathState{path: cleanPath, modTime: 0, size: 0, exists: false, dir: false}
	}

	return ignorePathState{
		path:    cleanPath,
		modTime: info.ModTime().UnixNano(),
		size:    info.Size(),
		exists:  true,
		dir:     info.IsDir(),
	}
}

func (signature repositoryIgnoreSignature) isFresh() bool {
	if !signature.complete {
		return false
	}

	for _, path := range signature.paths {
		if readIgnorePathState(path.path) != path {
			return false
		}
	}

	return true
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
