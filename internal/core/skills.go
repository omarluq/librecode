package core

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/samber/lo"
	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

const (
	maxSkillNameLength        = 64
	maxSkillDescriptionLength = 1024
	maxSkillCompatibilitySize = 500
	maxActiveSkills           = 3
	maxActiveSkillContent     = 20_000
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

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

type skillFrontmatter struct {
	Metadata               map[string]any `yaml:"metadata"`
	Name                   string         `yaml:"name"`
	Description            string         `yaml:"description"`
	License                string         `yaml:"license"`
	Compatibility          string         `yaml:"compatibility"`
	AllowedTools           allowedTools   `yaml:"allowed-tools"`
	UserInvocable          bool           `yaml:"user-invocable"`
	DisableModelInvocation bool           `yaml:"disable-model-invocation"`
}

type allowedTools []string

func (tools *allowedTools) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Tag == "!!null" {
		*tools = []string{}
		return nil
	}

	switch value.Kind {
	case yaml.ScalarNode:
		*tools = strings.Fields(value.Value)
		return nil
	case yaml.SequenceNode:
		parsed := make([]string, 0, len(value.Content))
		for _, item := range value.Content {
			if item.Kind != yaml.ScalarNode {
				return fmt.Errorf("allowed-tools entries must be strings")
			}
			trimmed := strings.TrimSpace(item.Value)
			if trimmed != "" {
				parsed = append(parsed, trimmed)
			}
		}
		*tools = parsed
		return nil
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
		return fmt.Errorf("allowed-tools must be a string or string list")
	default:
		return fmt.Errorf("allowed-tools has unsupported YAML kind %d", value.Kind)
	}
}

type skillDiscoveryState struct {
	seenDirs map[string]bool
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

// FormatSkillsForPrompt formats skill metadata in librecode's XML prompt block.
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

// AutoActivateSkills selects matching skills and reads their SKILL.md content for prompt context.
func AutoActivateSkills(prompt string, skills []Skill) ([]ActivatedSkill, []ResourceDiagnostic) {
	ranked := rankSkillsForPrompt(prompt, skills)
	if len(ranked) == 0 {
		return []ActivatedSkill{}, []ResourceDiagnostic{}
	}
	if len(ranked) > maxActiveSkills {
		ranked = ranked[:maxActiveSkills]
	}

	activated := make([]ActivatedSkill, 0, len(ranked))
	diagnostics := []ResourceDiagnostic{}
	for index := range ranked {
		skill := &ranked[index]
		content, err := readResourceFile(skill.FilePath)
		if err != nil {
			diagnostics = append(diagnostics, warningDiagnostic(err.Error(), skill.FilePath))
			continue
		}
		content, truncated := truncateSkillContent(content)
		activated = append(activated, ActivatedSkill{Skill: *skill, Content: content, Truncated: truncated})
	}

	return activated, diagnostics
}

// SkillContent reads one skill file's full Markdown content.
func SkillContent(skill *Skill) (string, error) {
	return readResourceFile(skill.FilePath)
}

// FormatActiveSkillsForPrompt formats full activated skill content for the model request.
func FormatActiveSkillsForPrompt(skills []ActivatedSkill) string {
	if len(skills) == 0 {
		return ""
	}

	lines := []string{
		"\n\nThe following skills were automatically activated for this request.",
		"Follow their instructions when relevant.",
		"",
		"<active_skills>",
	}
	for index := range skills {
		activation := &skills[index]
		lines = append(lines,
			"  <skill>",
			fmt.Sprintf("    <name>%s</name>", html.EscapeString(activation.Skill.Name)),
			fmt.Sprintf("    <location>%s</location>", html.EscapeString(activation.Skill.FilePath)),
		)
		if activation.Truncated {
			lines = append(lines, "    <truncated>true</truncated>")
		}
		lines = append(lines,
			"    <content>",
			html.EscapeString(activation.Content),
			"    </content>",
			"  </skill>",
		)
	}
	lines = append(lines, "</active_skills>")

	return strings.Join(lines, "\n")
}

func loadSkillPath(rawPath, cwd string) LoadSkillsResult {
	resolvedPath := resolveResourcePath(rawPath, cwd)
	info, err := statResource(resolvedPath)
	if err != nil {
		return LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}
	}
	if info.IsDir() {
		return loadSkillsFromDir(resolvedPath, cwd, false)
	}
	if info.Mode().IsRegular() && strings.HasSuffix(resolvedPath, ".md") {
		skill, diagnostics := loadSkillFromFile(resolvedPath, cwd)
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

func loadSkillsFromDir(dir, cwd string, includeLegacyRootFiles bool) LoadSkillsResult {
	state := &skillDiscoveryState{seenDirs: map[string]bool{}}

	return loadSkillsFromDirWithState(dir, cwd, includeLegacyRootFiles, state)
}

func loadSkillsFromDirWithState(
	dir string,
	cwd string,
	includeLegacyRootFiles bool,
	state *skillDiscoveryState,
) LoadSkillsResult {
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

	return loadNestedSkills(entries, dir, cwd, includeLegacyRootFiles, state)
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

func loadNestedSkills(
	entries []os.DirEntry,
	dir string,
	cwd string,
	includeLegacyRootFiles bool,
	state *skillDiscoveryState,
) LoadSkillsResult {
	result := LoadSkillsResult{Skills: []Skill{}, Diagnostics: []ResourceDiagnostic{}}
	ignorePatterns := readSkillIgnorePatterns(dir)
	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())
		if shouldSkipSkillEntry(entry, entryPath, ignorePatterns) {
			continue
		}
		if isSkillDirEntry(entry, entryPath) {
			nested := loadSkillsFromDirWithState(entryPath, cwd, false, state)
			result.Skills = append(result.Skills, nested.Skills...)
			result.Diagnostics = append(result.Diagnostics, nested.Diagnostics...)
			continue
		}
		if includeLegacyRootFiles && strings.HasSuffix(entry.Name(), ".md") {
			skill, diagnostics := loadSkillFromFile(entryPath, cwd)
			result.Diagnostics = append(result.Diagnostics, diagnostics...)
			if skill != nil {
				result.Skills = append(result.Skills, *skill)
			}
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

func skillDiagnostics(filePath, name, parentDirName string, frontmatter *skillFrontmatter) []ResourceDiagnostic {
	messages := append(validateSkillName(name, parentDirName), validateSkillDescription(frontmatter.Description)...)
	messages = append(messages, validateSkillCompatibility(frontmatter.Compatibility)...)

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

func validateSkillCompatibility(compatibility string) []string {
	if len(compatibility) <= maxSkillCompatibilitySize {
		return []string{}
	}

	return []string{fmt.Sprintf(
		"compatibility exceeds %d characters (%d)",
		maxSkillCompatibilitySize,
		len(compatibility),
	)}
}

func shouldSkipSkillEntry(entry os.DirEntry, path string, ignorePatterns []string) bool {
	name := entry.Name()
	if strings.HasPrefix(name, ".") && !isSkillIgnoreFile(name) {
		return true
	}
	if name == "node_modules" {
		return true
	}

	return matchesSkillIgnore(name, path, ignorePatterns)
}

func isSkillDirEntry(entry os.DirEntry, path string) bool {
	if entry.IsDir() {
		return true
	}
	info, err := statResource(path)

	return err == nil && info.IsDir()
}

func readSkillIgnorePatterns(dir string) []string {
	patterns := []string{}
	for _, filename := range []string{".gitignore", ".ignore", ".fdignore"} {
		content, err := readResourceFile(filepath.Join(dir, filename))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(content, "\n") {
			pattern := strings.TrimSpace(line)
			if pattern == "" || strings.HasPrefix(pattern, "#") || strings.HasPrefix(pattern, "!") {
				continue
			}
			patterns = append(patterns, pattern)
		}
	}

	return patterns
}

func matchesSkillIgnore(name, path string, patterns []string) bool {
	slashPath := filepath.ToSlash(path)
	for _, pattern := range patterns {
		trimmed := strings.Trim(pattern, "/")
		if trimmed == "" {
			continue
		}
		if trimmed == name || strings.HasSuffix(slashPath, "/"+trimmed) {
			return true
		}
		if matched, err := filepath.Match(trimmed, name); err == nil && matched {
			return true
		}
		if matched, err := filepath.Match(trimmed, slashPath); err == nil && matched {
			return true
		}
	}

	return false
}

func isSkillIgnoreFile(name string) bool {
	return name == ".gitignore" || name == ".ignore" || name == ".fdignore"
}

func sourceInfoForSkill(filePath, cwd string) SourceInfo {
	scope := SourceScopeTemporary
	baseDir := filepath.Dir(filePath)
	for _, projectSkillsDir := range projectSkillPaths(cwd) {
		if isUnderPath(filePath, projectSkillsDir) {
			scope = SourceScopeProject
			baseDir = projectSkillsDir
			break
		}
	}
	if scope == SourceScopeTemporary {
		for _, userSkillsDir := range userSkillPaths() {
			if isUnderPath(filePath, userSkillsDir) {
				scope = SourceScopeUser
				baseDir = userSkillsDir
				break
			}
		}
	}

	return NewSourceInfo(filePath, SourceInfoOptions{
		Scope:   scope,
		Origin:  SourceOriginTopLevel,
		BaseDir: baseDir,
		Source:  resourceSourceLocal,
	})
}

type rankedSkill struct {
	skill Skill
	score int
	order int
}

func rankSkillsForPrompt(prompt string, skills []Skill) []Skill {
	ranked := []rankedSkill{}
	promptTokens := tokenSet(prompt)
	promptLower := strings.ToLower(prompt)
	for index := range skills {
		skill := skills[index]
		if skill.DisableModelInvocation {
			continue
		}
		score := skillActivationScore(promptLower, promptTokens, &skill)
		if score < 3 {
			continue
		}
		ranked = append(ranked, rankedSkill{skill: skill, score: score, order: index})
	}
	sort.SliceStable(ranked, func(leftIndex, rightIndex int) bool {
		left := ranked[leftIndex]
		right := ranked[rightIndex]
		if left.score == right.score {
			return left.order < right.order
		}

		return left.score > right.score
	})

	return lo.Map(ranked, func(item rankedSkill, _ int) Skill { return item.skill })
}

func skillActivationScore(promptLower string, promptTokens map[string]bool, skill *Skill) int {
	score := 0
	nameLower := strings.ToLower(skill.Name)
	if strings.Contains(promptLower, nameLower) {
		score += 5
	}
	nameTokens := strings.Split(nameLower, "-")
	nameTokenMatches := 0
	for _, token := range nameTokens {
		if len(token) < 4 {
			continue
		}
		if promptTokens[token] {
			nameTokenMatches++
			score += 2
		}
	}
	if nameTokenMatches == len(lo.Filter(nameTokens, func(token string, _ int) bool { return len(token) >= 4 })) {
		score += 3
	}
	for token := range tokenSet(skill.Description) {
		if isSkillStopWord(token) || len(token) < 5 {
			continue
		}
		if promptTokens[token] {
			score++
		}
	}

	return score
}

func tokenSet(input string) map[string]bool {
	tokens := map[string]bool{}
	fields := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, field := range fields {
		if field != "" {
			tokens[field] = true
		}
	}

	return tokens
}

func isSkillStopWord(token string) bool {
	stopWords := map[string]bool{
		"about": true, "after": true, "agent": true, "apply": true, "build": true,
		"code": true, "coding": true, "cover": true, "covers": true, "debug": true,
		"designed": true, "especially": true, "guide": true, "helps": true, "implement": true,
		"project": true, "provides": true, "review": true, "similar": true, "skill": true,
		"tasks": true, "their": true, "these": true, "tools": true, "trigger": true,
		"using": true, "whenever": true, "working": true, "write": true, "writing": true,
	}

	return stopWords[token]
}

func truncateSkillContent(content string) (string, bool) {
	runes := []rune(content)
	if len(runes) <= maxActiveSkillContent {
		return content, false
	}

	return string(runes[:maxActiveSkillContent]) + "\n\n[skill content truncated]", true
}
