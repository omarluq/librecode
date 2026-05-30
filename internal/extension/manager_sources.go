package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"
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
	manifestPath := filepath.Join(root, "init.lua")
	if info, err := os.Stat(manifestPath); err == nil && !info.IsDir() {
		return []luaSource{{Path: manifestPath, Manifest: true}}, nil
	}

	sources := []luaSource{}
	walkErr := filepath.WalkDir(root, func(currentPath string, dirEntry os.DirEntry, walkErr error) error {
		return collectLuaSource(root, currentPath, dirEntry, walkErr, &sources)
	})
	if walkErr != nil {
		return nil, fmt.Errorf("extension: walk %s: %w", root, walkErr)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })

	return sources, nil
}

func collectLuaSource(root, currentPath string, dirEntry os.DirEntry, walkErr error, sources *[]luaSource) error {
	if walkErr != nil {
		return walkErr
	}
	if isExtensionDir(root, currentPath, dirEntry) {
		return collectLuaSourceDir(root, currentPath, sources)
	}
	if strings.HasSuffix(currentPath, ".lua") {
		*sources = append(*sources, luaSource{Path: currentPath, Manifest: false})
	}

	return nil
}

func isExtensionDir(root, currentPath string, dirEntry os.DirEntry) bool {
	if dirEntry.IsDir() {
		return true
	}
	if currentPath == root || dirEntry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(currentPath)

	return err == nil && info.IsDir()
}

func collectLuaSourceDir(root, currentPath string, sources *[]luaSource) error {
	if currentPath == root {
		return nil
	}
	if filepath.Base(currentPath) == "lua" {
		return filepath.SkipDir
	}
	manifestPath := filepath.Join(currentPath, "init.lua")
	if info, err := os.Stat(manifestPath); err == nil && !info.IsDir() {
		*sources = append(*sources, luaSource{Path: manifestPath, Manifest: true})
		return filepath.SkipDir
	}

	return nil
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
			filepath.ToSlash(filepath.Join(root, "?", "init.lua")),
		)
	}
	packageTable.RawSetString("path", lua.LString(strings.Join(patterns, ";")))
}

func (manager *Manager) moduleRootsSnapshot() []string {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	return append([]string{}, manager.moduleRoots...)
}
