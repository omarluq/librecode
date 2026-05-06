package core

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

const (
	maxSkillNameLength        = 64
	maxSkillDescriptionLength = 1024
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Skill describes one Agent Skills compatible skill file.
type Skill struct {
	SourceInfo             SourceInfo `json:"sourceInfo"`
	Name                   string     `json:"name"`
	Description            string     `json:"description"`
	FilePath               string     `json:"filePath"`
	BaseDir                string     `json:"baseDir"`
	DisableModelInvocation bool       `json:"disableModelInvocation"`
}

// LoadSkillsResult returns loaded skills plus validation diagnostics.
type LoadSkillsResult struct {
	Skills      []Skill              `json:"skills"`
	Diagnostics []ResourceDiagnostic `json:"diagnostics"`
}

type skillFrontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
}

// LoadSkills loads skills from default and explicit paths.
func LoadSkills(cwd, agentDir string, skillPaths []string, includeDefaults bool) LoadSkillsResult {
	paths := []string{}
	if includeDefaults {
		paths = append(paths, filepath.Join(agentDir, skillDirName), filepath.Join(cwd, ConfigDirName, skillDirName))
	}
	paths = append(paths, skillPaths...)

	result := LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}
	seenFiles := map[string]bool{}
	skillsByName := map[string]Skill{}
	for _, rawPath := range paths {
		pathResult := loadSkillPath(rawPath, cwd, agentDir)
		result.Diagnostics = append(result.Diagnostics, pathResult.Diagnostics...)
		mergeSkills(&result, skillsByName, seenFiles, pathResult.Skills)
	}

	return result
}

// FormatSkillsForPrompt formats skills in librecode's XML prompt block.
func FormatSkillsForPrompt(skills []Skill) string {
	visibleSkills := lo.Filter(skills, func(skill Skill, _ int) bool {
		return !skill.DisableModelInvocation
	})
	if len(visibleSkills) == 0 {
		return ""
	}

	lines := []string{
		"\n\nThe following skills provide specialized instructions for specific tasks.",
		"Use the read tool to load a skill's file when the task matches its description.",
		"When a skill file references a relative path, resolve it against the skill directory " +
			"(parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.",
		"",
		"<available_skills>",
	}
	for index := range visibleSkills {
		skill := &visibleSkills[index]
		lines = append(lines,
			"  <skill>",
			fmt.Sprintf("    <name>%s</name>", html.EscapeString(skill.Name)),
			fmt.Sprintf("    <description>%s</description>", html.EscapeString(skill.Description)),
			fmt.Sprintf("    <location>%s</location>", html.EscapeString(skill.FilePath)),
			"  </skill>",
		)
	}
	lines = append(lines, "</available_skills>")

	return strings.Join(lines, "\n")
}

func loadSkillPath(rawPath, cwd, agentDir string) LoadSkillsResult {
	resolvedPath := resolveResourcePath(rawPath, cwd)
	info, err := statResource(resolvedPath)
	if err != nil {
		return LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}
	}
	if info.IsDir() {
		return loadSkillsFromDir(resolvedPath, cwd, agentDir, true)
	}
	if info.Mode().IsRegular() && strings.HasSuffix(resolvedPath, ".md") {
		skill, diagnostics := loadSkillFromFile(resolvedPath, cwd, agentDir)
		if skill == nil {
			return LoadSkillsResult{Skills: []Skill{}, Diagnostics: diagnostics}
		}

		return LoadSkillsResult{Skills: []Skill{*skill}, Diagnostics: diagnostics}
	}

	return LoadSkillsResult{
		Skills:      []Skill{},
		Diagnostics: []ResourceDiagnostic{warningDiagnostic("skill path is not a markdown file", resolvedPath)},
	}
}

func loadSkillsFromDir(dir, cwd, agentDir string, includeRootFiles bool) LoadSkillsResult {
	entries, err := readResourceDir(dir)
	if err != nil {
		return LoadSkillsResult{
			Skills:      []Skill{},
			Diagnostics: []ResourceDiagnostic{warningDiagnostic(err.Error(), dir)},
		}
	}
	if skillResult, found := loadSkillRoot(entries, dir, cwd, agentDir); found {
		return skillResult
	}

	return loadNestedSkills(entries, dir, cwd, agentDir, includeRootFiles)
}

func loadSkillRoot(entries []os.DirEntry, dir, cwd, agentDir string) (LoadSkillsResult, bool) {
	entry, found := lo.Find(entries, func(entry os.DirEntry) bool {
		return entry.Name() == skillFileName && !entry.IsDir()
	})
	if !found {
		return LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}, false
	}
	skillPath := filepath.Join(dir, entry.Name())
	skill, diagnostics := loadSkillFromFile(skillPath, cwd, agentDir)
	if skill == nil {
		return LoadSkillsResult{Skills: []Skill{}, Diagnostics: diagnostics}, true
	}

	return LoadSkillsResult{Skills: []Skill{*skill}, Diagnostics: diagnostics}, true
}

func loadNestedSkills(entries []os.DirEntry, dir, cwd, agentDir string, includeRootFiles bool) LoadSkillsResult {
	result := LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}
	for _, entry := range entries {
		if shouldSkipSkillEntry(entry) {
			continue
		}
		entryPath := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			nested := loadSkillsFromDir(entryPath, cwd, agentDir, false)
			result.Skills = append(result.Skills, nested.Skills...)
			result.Diagnostics = append(result.Diagnostics, nested.Diagnostics...)
			continue
		}
		if includeRootFiles && strings.HasSuffix(entry.Name(), ".md") {
			skill, diagnostics := loadSkillFromFile(entryPath, cwd, agentDir)
			result.Diagnostics = append(result.Diagnostics, diagnostics...)
			if skill != nil {
				result.Skills = append(result.Skills, *skill)
			}
		}
	}

	return result
}

func loadSkillFromFile(filePath, cwd, agentDir string) (*Skill, []ResourceDiagnostic) {
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
	diagnostics := skillDiagnostics(filePath, name, filepath.Base(skillDir), frontmatter.Description)
	if strings.TrimSpace(frontmatter.Description) == "" {
		return nil, diagnostics
	}

	return &Skill{
		SourceInfo:             sourceInfoForSkill(filePath, cwd, agentDir),
		Name:                   name,
		Description:            frontmatter.Description,
		FilePath:               filePath,
		BaseDir:                skillDir,
		DisableModelInvocation: frontmatter.DisableModelInvocation,
	}, diagnostics
}

func mergeSkills(result *LoadSkillsResult, skillsByName map[string]Skill, seenFiles map[string]bool, skills []Skill) {
	for index := range skills {
		skill := skills[index]
		realPath := canonicalizeResourcePath(skill.FilePath)
		if seenFiles[realPath] {
			continue
		}
		if existing, ok := skillsByName[skill.Name]; ok {
			result.Diagnostics = append(result.Diagnostics, collisionDiagnostic(&skill, &existing))
			continue
		}
		skillsByName[skill.Name] = skill
		seenFiles[realPath] = true
		result.Skills = append(result.Skills, skill)
	}
}

func collisionDiagnostic(skill, existing *Skill) ResourceDiagnostic {
	return collisionResourceDiagnostic(resourceTypeSkill, skill.Name, existing.FilePath, skill.FilePath)
}

func skillDiagnostics(filePath, name, parentDirName, description string) []ResourceDiagnostic {
	messages := append(validateSkillName(name, parentDirName), validateSkillDescription(description)...)

	return lo.Map(messages, func(message string, _ int) ResourceDiagnostic {
		return warningDiagnostic(message, filePath)
	})
}

func validateSkillName(name, parentDirName string) []string {
	errors := []string{}
	if name != parentDirName {
		errors = append(errors, fmt.Sprintf("name %q does not match parent directory %q", name, parentDirName))
	}
	if len(name) > maxSkillNameLength {
		errors = append(errors, fmt.Sprintf("name exceeds %d characters (%d)", maxSkillNameLength, len(name)))
	}
	if !skillNamePattern.MatchString(name) {
		errors = append(errors,
			"name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)",
		)
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		errors = append(errors, "name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		errors = append(errors, "name must not contain consecutive hyphens")
	}

	return errors
}

func validateSkillDescription(description string) []string {
	if strings.TrimSpace(description) == "" {
		return []string{"description is required"}
	}
	if len(description) > maxSkillDescriptionLength {
		message := fmt.Sprintf(
			"description exceeds %d characters (%d)",
			maxSkillDescriptionLength,
			len(description),
		)

		return []string{message}
	}

	return []string{}
}

func shouldSkipSkillEntry(entry os.DirEntry) bool {
	return strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules"
}

func sourceInfoForSkill(filePath, cwd, agentDir string) SourceInfo {
	globalSkillsDir := filepath.Join(agentDir, skillDirName)
	projectSkillsDir := filepath.Join(cwd, ConfigDirName, skillDirName)
	scope := SourceScopeTemporary
	baseDir := filepath.Dir(filePath)
	if isUnderPath(filePath, globalSkillsDir) {
		scope = SourceScopeUser
		baseDir = globalSkillsDir
	} else if isUnderPath(filePath, projectSkillsDir) {
		scope = SourceScopeProject
		baseDir = projectSkillsDir
	}

	return NewSourceInfo(filePath, SourceInfoOptions{
		Scope:   scope,
		Origin:  SourceOriginTopLevel,
		BaseDir: baseDir,
		Source:  resourceSourceLocal,
	})
}
