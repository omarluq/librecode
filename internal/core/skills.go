package core

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	maxSkillNameLength        = 64
	maxSkillDescriptionLength = 1024
	maxSkillCompatibilitySize = 500
	maxActiveSkills           = 3
	maxActiveSkillContent     = 20_000
)

// Skill describes one Agent Skills compatible skill file.
type Skill struct {
	Metadata               map[string]any `json:"metadata,omitempty"`
	SourceInfo             SourceInfo     `json:"sourceInfo"`
	Name                   string         `json:"name"`
	Description            string         `json:"description"`
	FilePath               string         `json:"filePath"`
	BaseDir                string         `json:"baseDir"`
	License                string         `json:"license,omitempty"`
	Compatibility          string         `json:"compatibility,omitempty"`
	AllowedTools           []string       `json:"allowedTools,omitempty"`
	UserInvocable          bool           `json:"userInvocable,omitempty"`
	DisableModelInvocation bool           `json:"disableModelInvocation"`
}

// ActivatedSkill contains full skill content selected for the current prompt.
type ActivatedSkill struct {
	Content   string `json:"content"`
	Skill     Skill  `json:"skill"`
	Truncated bool   `json:"truncated"`
}

// LoadSkillsResult returns loaded skills plus validation diagnostics.
type LoadSkillsResult struct {
	Skills      []Skill              `json:"skills"`
	Diagnostics []ResourceDiagnostic `json:"diagnostics"`
}

// LoadSkills loads skills from the four supported default roots and explicit paths.
func LoadSkills(cwd string, skillPaths []string, includeDefaults bool) LoadSkillsResult {
	paths := []string{}
	if includeDefaults {
		paths = append(paths, defaultSkillPaths(cwd)...)
	}
	paths = append(paths, skillPaths...)

	result := LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}
	seenFiles := map[string]bool{}
	skillsByName := map[string]Skill{}
	for _, rawPath := range paths {
		pathResult := loadSkillPath(rawPath, cwd)
		result.Diagnostics = append(result.Diagnostics, pathResult.Diagnostics...)
		mergeSkills(&result, skillsByName, seenFiles, pathResult.Skills)
	}

	return result
}

func defaultSkillPaths(cwd string) []string {
	paths := append([]string{}, projectSkillPaths(cwd)...)
	paths = append(paths, userSkillPaths()...)

	return dedupeSkillPaths(paths)
}

func projectSkillPaths(cwd string) []string {
	return []string{
		filepath.Join(cwd, ConfigDirName, skillDirName),
		filepath.Join(cwd, AgentsDirName, skillDirName),
	}
}

func userSkillPaths() []string {
	paths := []string{}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		paths = append(paths,
			filepath.Join(home, ConfigDirName, skillDirName),
			filepath.Join(home, AgentsDirName, skillDirName),
		)
	}

	return dedupeSkillPaths(paths)
}

func dedupeSkillPaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, rawPath := range paths {
		if strings.TrimSpace(rawPath) == "" {
			continue
		}
		canonicalPath := canonicalizeResourcePath(rawPath)
		if seen[canonicalPath] {
			continue
		}
		seen[canonicalPath] = true
		result = append(result, rawPath)
	}

	return result
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

// SkillContent reads one skill file's full Markdown content.
func SkillContent(skill *Skill) (string, error) {
	return readResourceFile(skill.FilePath)
}
