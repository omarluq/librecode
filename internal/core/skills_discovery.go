package core

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

type skillDiscoveryState struct {
	seenDirs map[string]bool
}

func loadSkillPath(rawPath, cwd string) LoadSkillsResult {
	resolvedPath := resolveResourcePath(rawPath, cwd)
	info, err := statResource(resolvedPath)
	if err != nil {
		return LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}
	}
	if info.IsDir() {
		return loadSkillsFromDir(resolvedPath, cwd)
	}

	return LoadSkillsResult{
		Skills:      []Skill{},
		Diagnostics: []ResourceDiagnostic{warningDiagnostic("skill path is not a directory", resolvedPath)},
	}
}

func loadSkillsFromDir(dir, cwd string) LoadSkillsResult {
	state := &skillDiscoveryState{seenDirs: map[string]bool{}}

	return loadSkillsFromDirWithState(dir, cwd, state)
}

func loadSkillsFromDirWithState(dir, cwd string, state *skillDiscoveryState) LoadSkillsResult {
	canonicalDir := canonicalizeResourcePath(dir)
	if state.seenDirs[canonicalDir] {
		return LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}
	}
	state.seenDirs[canonicalDir] = true

	entries, err := readResourceDir(dir)
	if err != nil {
		return LoadSkillsResult{
			Skills:      []Skill{},
			Diagnostics: []ResourceDiagnostic{warningDiagnostic(err.Error(), dir)},
		}
	}
	if skillResult, found := loadSkillRoot(entries, dir, cwd); found {
		return skillResult
	}

	return loadNestedSkills(entries, dir, cwd, state)
}

func loadSkillRoot(entries []os.DirEntry, dir, cwd string) (LoadSkillsResult, bool) {
	entry, found := lo.Find(entries, func(entry os.DirEntry) bool {
		return entry.Name() == skillFileName && !entry.IsDir()
	})
	if !found {
		return LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}, false
	}
	skillPath := filepath.Join(dir, entry.Name())
	skill, diagnostics := loadSkillFromFile(skillPath, cwd)
	if skill == nil {
		return LoadSkillsResult{Skills: []Skill{}, Diagnostics: diagnostics}, true
	}

	return LoadSkillsResult{Skills: []Skill{*skill}, Diagnostics: diagnostics}, true
}

func loadNestedSkills(entries []os.DirEntry, dir, cwd string, state *skillDiscoveryState) LoadSkillsResult {
	result := LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}
	ignorePatterns := readSkillIgnorePatterns(dir)
	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())
		if shouldSkipSkillEntry(entry, entryPath, ignorePatterns) {
			continue
		}
		if isSkillDirEntry(entry, entryPath) {
			nested := loadSkillsFromDirWithState(entryPath, cwd, state)
			result.Skills = append(result.Skills, nested.Skills...)
			result.Diagnostics = append(result.Diagnostics, nested.Diagnostics...)
			continue
		}
	}

	return result
}

func loadSkillFromFile(filePath, cwd string) (*Skill, []ResourceDiagnostic) {
	content, err := readResourceFile(filePath)
	if err != nil {
		return nil, []ResourceDiagnostic{warningDiagnostic(err.Error(), filePath)}
	}
	frontmatter, _, err := ParseFrontmatter[skillFrontmatter](content)
	if err != nil {
		wrapped := oops.In("core").Code("skill_frontmatter").Wrapf(err, "parse skill frontmatter")
		return nil, []ResourceDiagnostic{warningDiagnostic(wrapped.Error(), filePath)}
	}

	skillDir := filepath.Dir(filePath)
	name := frontmatter.Name
	if name == "" {
		name = filepath.Base(skillDir)
	}
	diagnostics := skillDiagnostics(filePath, name, filepath.Base(skillDir), &frontmatter)
	if strings.TrimSpace(frontmatter.Description) == "" {
		return nil, diagnostics
	}

	return &Skill{
		Metadata:               frontmatter.Metadata,
		SourceInfo:             sourceInfoForSkill(filePath, cwd),
		Name:                   name,
		Description:            frontmatter.Description,
		FilePath:               filePath,
		BaseDir:                skillDir,
		License:                frontmatter.License,
		Compatibility:          frontmatter.Compatibility,
		AllowedTools:           []string(frontmatter.AllowedTools),
		UserInvocable:          frontmatter.UserInvocable,
		DisableModelInvocation: frontmatter.DisableModelInvocation,
	}, diagnostics
}
