package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/omarluq/librecode/internal/fswalk"
	lua "github.com/yuin/gopher-lua"
)

const (
	luaManifestFile        = "init.lua"
	minLuaSourcesForDedupe = 2
)

type luaSource struct {
	Path     string
	Manifest bool
}

func discoverLuaSources(extensionPath string) ([]luaSource, error) {
	if extensionPath == "" {
		return []luaSource{}, nil
	}

	info, err := os.Stat(extensionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []luaSource{}, nil
		}

		return nil, fmt.Errorf("extension: stat %s: %w", extensionPath, err)
	}

	if !info.IsDir() {
		if strings.HasSuffix(extensionPath, ".lua") {
			return []luaSource{{Path: extensionPath, Manifest: false}}, nil
		}

		return []luaSource{}, nil
	}

	return discoverLuaDir(extensionPath)
}

func discoverLuaDir(root string) ([]luaSource, error) {
	manifestPath := filepath.Join(root, luaManifestFile)
	if info, err := os.Stat(manifestPath); err == nil && !info.IsDir() {
		return []luaSource{{Path: manifestPath, Manifest: true}}, nil
	}

	sources := []luaSource{}

	var sourcesLock sync.Mutex

	walkErr := fswalk.Walk(root, func(currentPath string, dirEntry os.DirEntry, walkErr error) error {
		return collectLuaSource(root, currentPath, dirEntry, walkErr, &sources, &sourcesLock)
	})
	if walkErr != nil {
		return nil, fmt.Errorf("extension: walk %s: %w", root, walkErr)
	}

	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })

	return dedupeLuaSourcesByTarget(sources), nil
}

func collectLuaSource(
	root string,
	currentPath string,
	dirEntry os.DirEntry,
	walkErr error,
	sources *[]luaSource,
	sourcesLock *sync.Mutex,
) error {
	if walkErr != nil {
		return walkErr
	}

	if isExtensionDir(root, currentPath, dirEntry) {
		return collectLuaSourceDir(root, currentPath, sources, sourcesLock)
	}

	if dirEntry.Type()&os.ModeSymlink != 0 && symlinkTargetsDirectory(currentPath) {
		return filepath.SkipDir
	}

	if strings.HasSuffix(currentPath, ".lua") {
		appendLuaSource(sources, sourcesLock, luaSource{Path: currentPath, Manifest: false})
	}

	return nil
}

func isExtensionDir(root, currentPath string, dirEntry os.DirEntry) bool {
	if dirEntry.IsDir() {
		return true
	}

	return currentPath != root && dirEntry.Type()&os.ModeSymlink != 0 && symlinkTargetsDirectory(currentPath)
}

func symlinkTargetsDirectory(currentPath string) bool {
	info, err := os.Stat(currentPath)

	return err == nil && info.IsDir()
}

func collectLuaSourceDir(root, currentPath string, sources *[]luaSource, sourcesLock *sync.Mutex) error {
	if currentPath == root {
		return nil
	}

	if filepath.Base(currentPath) == "lua" {
		return filepath.SkipDir
	}

	manifestPath := filepath.Join(currentPath, luaManifestFile)
	if info, err := os.Stat(manifestPath); err == nil && !info.IsDir() {
		appendLuaSource(sources, sourcesLock, luaSource{Path: manifestPath, Manifest: true})

		return filepath.SkipDir
	}

	return nil
}

func appendLuaSource(sources *[]luaSource, sourcesLock *sync.Mutex, source luaSource) {
	sourcesLock.Lock()
	defer sourcesLock.Unlock()

	*sources = append(*sources, source)
}

func dedupeLuaSourcesByTarget(sources []luaSource) []luaSource {
	if len(sources) < minLuaSourcesForDedupe {
		return sources
	}

	deduped := make([]luaSource, 0, len(sources))
	seen := map[string]struct{}{}

	for _, source := range sources {
		targetPath, err := filepath.EvalSymlinks(source.Path)
		if err != nil {
			targetPath = source.Path
		}

		if _, ok := seen[targetPath]; ok {
			continue
		}

		seen[targetPath] = struct{}{}

		deduped = append(deduped, source)
	}

	return deduped
}

func (manager *Manager) addModuleRootsForPath(extensionPath string) {
	if strings.TrimSpace(extensionPath) == "" {
		return
	}

	absolutePath, err := filepath.Abs(extensionPath)
	if err != nil {
		absolutePath = extensionPath
	}

	roots := moduleRootsForPath(absolutePath)

	manager.lock.Lock()
	defer manager.lock.Unlock()

	seen := make(map[string]struct{}, len(manager.moduleRoots)+len(roots))
	for _, root := range manager.moduleRoots {
		seen[root] = struct{}{}
	}

	for _, root := range roots {
		if _, ok := seen[root]; ok {
			continue
		}

		manager.moduleRoots = append(manager.moduleRoots, root)
		seen[root] = struct{}{}
	}
}

func moduleRootsForPath(extensionPath string) []string {
	root := extensionPath
	if info, err := os.Stat(extensionPath); err == nil && !info.IsDir() {
		root = filepath.Dir(extensionPath)
	}

	return []string{root}
}

func (manager *Manager) configurePackagePath(state *lua.LState) {
	packageTable, ok := state.GetGlobal("package").(*lua.LTable)
	if !ok {
		return
	}

	patterns := []string{packageTable.RawGetString("path").String()}
	for _, root := range manager.moduleRootsSnapshot() {
		patterns = append(patterns,
			filepath.ToSlash(filepath.Join(root, "?.lua")),
			filepath.ToSlash(filepath.Join(root, "?", luaManifestFile)),
		)
	}

	packageTable.RawSetString("path", lua.LString(strings.Join(patterns, ";")))
}

func (manager *Manager) moduleRootsSnapshot() []string {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	return append([]string{}, manager.moduleRoots...)
}
