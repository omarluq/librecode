package core

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/samber/lo"
)

const (
	systemPromptFileName       = "SYSTEM.md"
	appendSystemPromptFileName = "APPEND_SYSTEM.md"
)

// ResourcePath is an extension-provided resource path plus optional source metadata.
type ResourcePath struct {
	SourceInfo *SourceInfo `json:"source_info,omitempty"`
	Path       string      `json:"path"`
}

// ResourceExtensionPaths contains runtime resource paths contributed by extensions.
type ResourceExtensionPaths struct {
	SkillPaths  []ResourcePath `json:"skill_paths"`
	PromptPaths []ResourcePath `json:"prompt_paths"`
}

// ResourceLoaderOptions configures DefaultResourceLoader.
type ResourceLoaderOptions struct {
	SystemPrompt                  string   `json:"system_prompt"`
	AgentDir                      string   `json:"agent_dir"`
	CWD                           string   `json:"cwd"`
	AdditionalSkillPaths          []string `json:"additional_skill_paths"`
	AdditionalPromptTemplatePaths []string `json:"additional_prompt_template_paths"`
	AppendSystemPrompt            []string `json:"append_system_prompt"`
	NoPromptTemplates             bool     `json:"no_prompt_templates"`
	NoContextFiles                bool     `json:"no_context_files"`
	NoSkills                      bool     `json:"no_skills"`
}

// ResourceSnapshot is a coherent immutable view of loaded resources.
type ResourceSnapshot struct {
	SkillDiagnostics   []ResourceDiagnostic `json:"skill_diagnostics"`
	PromptDiagnostics  []ResourceDiagnostic `json:"prompt_diagnostics"`
	AppendSystemPrompt []string             `json:"append_system_prompt"`
	ContextFiles       []ContextFile        `json:"context_files"`
	SystemPrompt       string               `json:"system_prompt,omitempty"`
	Skills             []Skill              `json:"skills"`
	Prompts            []PromptTemplate     `json:"prompts"`
}

// ResourceLoader exposes librecode-style reloadable prompt, skill, and context resources.
type ResourceLoader interface {
	Reload(ctx context.Context) error
	ExtendResources(ctx context.Context, paths ResourceExtensionPaths) error
	Snapshot() ResourceSnapshot
	Skills() LoadSkillsResult
	Prompts() LoadPromptTemplatesResult
	ContextFiles() []ContextFile
	SystemPrompt() string
	AppendSystemPrompt() []string
}

// DefaultResourceLoader loads local librecode-compatible resources from user, project, and explicit paths.
type DefaultResourceLoader struct {
	promptSourceInfos             map[string]SourceInfo
	skillSourceInfos              map[string]SourceInfo
	systemPrompt                  string
	cwd                           string
	agentDir                      string
	snapshot                      ResourceSnapshot
	additionalSkillPaths          []string
	appendSystemPrompt            []string
	additionalPromptTemplatePaths []string
	lock                          sync.RWMutex
	noPromptTemplates             bool
	noContextFiles                bool
	noSkills                      bool
}

// NewDefaultResourceLoader creates a resource loader with librecode-compatible defaults.
func NewDefaultResourceLoader(options *ResourceLoaderOptions) *DefaultResourceLoader {
	resolvedOptions := resourceLoaderOptions(options)

	return &DefaultResourceLoader{
		skillSourceInfos:              map[string]SourceInfo{},
		promptSourceInfos:             map[string]SourceInfo{},
		snapshot:                      emptyResourceSnapshot(),
		additionalSkillPaths:          append([]string{}, resolvedOptions.AdditionalSkillPaths...),
		additionalPromptTemplatePaths: append([]string{}, resolvedOptions.AdditionalPromptTemplatePaths...),
		appendSystemPrompt:            append([]string{}, resolvedOptions.AppendSystemPrompt...),
		systemPrompt:                  resolvedOptions.SystemPrompt,
		agentDir:                      resolvedOptions.AgentDir,
		cwd:                           resolvedOptions.CWD,
		lock:                          sync.RWMutex{},
		noPromptTemplates:             resolvedOptions.NoPromptTemplates,
		noContextFiles:                resolvedOptions.NoContextFiles,
		noSkills:                      resolvedOptions.NoSkills,
	}
}

// Reload refreshes all resource snapshots from disk.
func (loader *DefaultResourceLoader) Reload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	snapshot := emptyResourceSnapshot()
	snapshot.Skills, snapshot.SkillDiagnostics = loader.loadSkillsSnapshot()
	snapshot.Prompts, snapshot.PromptDiagnostics = loader.loadPromptsSnapshot()
	snapshot.ContextFiles = loader.loadContextFilesSnapshot()
	snapshot.SystemPrompt = loader.loadSystemPrompt()
	snapshot.AppendSystemPrompt = loader.loadAppendSystemPrompts()

	loader.lock.Lock()
	loader.snapshot = snapshot
	loader.lock.Unlock()

	return ctx.Err()
}

// ExtendResources adds runtime resource paths and reloads affected resources.
func (loader *DefaultResourceLoader) ExtendResources(ctx context.Context, paths ResourceExtensionPaths) error {
	loader.lock.Lock()
	loader.addRuntimeResources(paths)
	loader.lock.Unlock()

	return loader.Reload(ctx)
}

// Snapshot returns a defensive copy of the current resource state.
func (loader *DefaultResourceLoader) Snapshot() ResourceSnapshot {
	loader.lock.RLock()
	defer loader.lock.RUnlock()

	return copyResourceSnapshot(&loader.snapshot)
}

// Skills returns loaded skills plus diagnostics.
func (loader *DefaultResourceLoader) Skills() LoadSkillsResult {
	snapshot := loader.Snapshot()

	return LoadSkillsResult{Skills: snapshot.Skills, Diagnostics: snapshot.SkillDiagnostics}
}

// Prompts returns loaded prompt templates plus diagnostics.
func (loader *DefaultResourceLoader) Prompts() LoadPromptTemplatesResult {
	snapshot := loader.Snapshot()

	return LoadPromptTemplatesResult{Prompts: snapshot.Prompts, Diagnostics: snapshot.PromptDiagnostics}
}

// ContextFiles returns loaded AGENTS.md/CLAUDE.md context files.
func (loader *DefaultResourceLoader) ContextFiles() []ContextFile {
	return loader.Snapshot().ContextFiles
}

// SystemPrompt returns a custom system prompt loaded from file or literal input.
func (loader *DefaultResourceLoader) SystemPrompt() string {
	return loader.Snapshot().SystemPrompt
}

// AppendSystemPrompt returns custom prompt fragments appended to the system prompt.
func (loader *DefaultResourceLoader) AppendSystemPrompt() []string {
	return loader.Snapshot().AppendSystemPrompt
}

// LoadProjectContextFiles loads global context and project ancestor AGENTS/CLAUDE files.
func LoadProjectContextFiles(cwd, agentDir string) []ContextFile {
	contextFiles := []ContextFile{}
	seenPaths := map[string]bool{}
	if contextFile, ok := loadContextFileFromDir(agentDir); ok {
		contextFiles = append(contextFiles, contextFile)
		seenPaths[canonicalizeResourcePath(contextFile.Path)] = true
	}

	ancestorFiles := loadAncestorContextFiles(cwd, seenPaths)
	contextFiles = append(contextFiles, ancestorFiles...)

	return contextFiles
}

// ResolvePromptInput treats an existing path as a prompt file and any other input as literal prompt text.
func ResolvePromptInput(input string) (string, bool) {
	if input == "" {
		return "", false
	}
	if !resourcePathExists(input) {
		return input, true
	}
	content, err := readResourceFile(input)
	if err != nil {
		return input, true
	}

	return content, true
}

func resourceLoaderOptions(options *ResourceLoaderOptions) *ResourceLoaderOptions {
	if options != nil {
		return options
	}

	return &ResourceLoaderOptions{
		AdditionalSkillPaths:          nil,
		AdditionalPromptTemplatePaths: nil,
		AppendSystemPrompt:            nil,
		SystemPrompt:                  "",
		AgentDir:                      "",
		CWD:                           "",
		NoPromptTemplates:             false,
		NoContextFiles:                false,
		NoSkills:                      false,
	}
}

func emptyResourceSnapshot() ResourceSnapshot {
	return ResourceSnapshot{
		SkillDiagnostics:   []ResourceDiagnostic{},
		PromptDiagnostics:  []ResourceDiagnostic{},
		AppendSystemPrompt: []string{},
		ContextFiles:       []ContextFile{},
		SystemPrompt:       "",
		Skills:             []Skill{},
		Prompts:            []PromptTemplate{},
	}
}

func (loader *DefaultResourceLoader) loadSkillsSnapshot() ([]Skill, []ResourceDiagnostic) {
	if loader.noSkills && len(loader.additionalSkillPaths) == 0 {
		return []Skill{}, []ResourceDiagnostic{}
	}
	result := LoadSkills(loader.cwd, loader.additionalSkillPaths, !loader.noSkills)
	result.Skills = loader.applySkillSourceInfo(result.Skills)
	result.Diagnostics = append(result.Diagnostics,
		missingResourceDiagnostics(loader.cwd, loader.additionalSkillPaths, resourceTypeSkill)...,
	)

	return result.Skills, result.Diagnostics
}

func (loader *DefaultResourceLoader) loadPromptsSnapshot() ([]PromptTemplate, []ResourceDiagnostic) {
	if loader.noPromptTemplates && len(loader.additionalPromptTemplatePaths) == 0 {
		return []PromptTemplate{}, []ResourceDiagnostic{}
	}
	result := LoadPromptTemplates(&LoadPromptTemplatesOptions{
		CWD:             loader.cwd,
		AgentDir:        loader.agentDir,
		PromptPaths:     loader.additionalPromptTemplatePaths,
		IncludeDefaults: !loader.noPromptTemplates,
	})
	result.Prompts = loader.applyPromptSourceInfo(result.Prompts)
	deduped := DedupePromptTemplates(result.Prompts)
	result.Prompts = deduped.Prompts
	result.Diagnostics = append(result.Diagnostics, deduped.Diagnostics...)
	result.Diagnostics = append(result.Diagnostics,
		missingResourceDiagnostics(loader.cwd, loader.additionalPromptTemplatePaths, resourceTypePrompt)...,
	)

	return result.Prompts, result.Diagnostics
}

func (loader *DefaultResourceLoader) loadContextFilesSnapshot() []ContextFile {
	if loader.noContextFiles {
		return []ContextFile{}
	}

	return LoadProjectContextFiles(loader.cwd, loader.agentDir)
}

func (loader *DefaultResourceLoader) loadSystemPrompt() string {
	source := loader.systemPrompt
	if source == "" {
		source = loader.discoverPromptFile(systemPromptFileName)
	}
	content, ok := ResolvePromptInput(source)
	if !ok {
		return ""
	}

	return content
}

func (loader *DefaultResourceLoader) loadAppendSystemPrompts() []string {
	sources := append([]string{}, loader.appendSystemPrompt...)
	if len(sources) == 0 {
		if discoveredPath := loader.discoverPromptFile(appendSystemPromptFileName); discoveredPath != "" {
			sources = append(sources, discoveredPath)
		}
	}

	return lo.FilterMap(sources, func(source string, _ int) (string, bool) {
		return ResolvePromptInput(source)
	})
}

func (loader *DefaultResourceLoader) discoverPromptFile(filename string) string {
	projectPath := filepath.Join(loader.cwd, ConfigDirName, filename)
	if resourcePathExists(projectPath) {
		return projectPath
	}
	globalPath := filepath.Join(loader.agentDir, filename)
	if resourcePathExists(globalPath) {
		return globalPath
	}

	return ""
}

func (loader *DefaultResourceLoader) addRuntimeResources(paths ResourceExtensionPaths) {
	for _, skillPath := range paths.SkillPaths {
		resolvedPath := resolveResourcePath(skillPath.Path, loader.cwd)
		loader.additionalSkillPaths = mergeResourcePaths(
			loader.cwd,
			loader.additionalSkillPaths,
			[]string{resolvedPath},
		)
		if skillPath.SourceInfo != nil {
			loader.skillSourceInfos[resolvedPath] = *skillPath.SourceInfo
		}
	}
	for _, promptPath := range paths.PromptPaths {
		resolvedPath := resolveResourcePath(promptPath.Path, loader.cwd)
		loader.additionalPromptTemplatePaths = mergeResourcePaths(
			loader.cwd,
			loader.additionalPromptTemplatePaths,
			[]string{resolvedPath},
		)
		if promptPath.SourceInfo != nil {
			loader.promptSourceInfos[resolvedPath] = *promptPath.SourceInfo
		}
	}
}

func (loader *DefaultResourceLoader) applySkillSourceInfo(skills []Skill) []Skill {
	return lo.Map(skills, func(skill Skill, _ int) Skill {
		if sourceInfo, ok := loader.findSourceInfo(skill.FilePath, loader.skillSourceInfos); ok {
			skill.SourceInfo = sourceInfo
		}

		return skill
	})
}

func (loader *DefaultResourceLoader) applyPromptSourceInfo(prompts []PromptTemplate) []PromptTemplate {
	return lo.Map(prompts, func(prompt PromptTemplate, _ int) PromptTemplate {
		if sourceInfo, ok := loader.findSourceInfo(prompt.FilePath, loader.promptSourceInfos); ok {
			prompt.SourceInfo = sourceInfo
		}

		return prompt
	})
}

func (loader *DefaultResourceLoader) findSourceInfo(
	resourcePath string,
	sourceInfos map[string]SourceInfo,
) (SourceInfo, bool) {
	resolvedResourcePath := canonicalizeResourcePath(resourcePath)
	for sourcePath, sourceInfo := range sourceInfos {
		resolvedSourcePath := canonicalizeResourcePath(sourcePath)
		if isUnderPath(resolvedResourcePath, resolvedSourcePath) {
			sourceInfo.Path = resourcePath
			return sourceInfo, true
		}
	}

	return SourceInfo{Path: "", Source: "", Scope: "", Origin: "", BaseDir: ""}, false
}

func loadContextFileFromDir(dir string) (ContextFile, bool) {
	for _, filename := range []string{"AGENTS.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD"} {
		filePath := filepath.Join(dir, filename)
		if !resourcePathExists(filePath) {
			continue
		}
		content, err := readResourceFile(filePath)
		if err != nil {
			continue
		}

		return ContextFile{Path: filePath, Content: content}, true
	}

	return ContextFile{Path: "", Content: ""}, false
}

func loadAncestorContextFiles(cwd string, seenPaths map[string]bool) []ContextFile {
	contextFiles := []ContextFile{}
	currentDir := filepath.Clean(cwd)
	rootDir := filepath.VolumeName(currentDir) + string(filepath.Separator)
	for {
		contextFiles = prependContextFile(contextFiles, currentDir, seenPaths)
		if currentDir == rootDir || filepath.Dir(currentDir) == currentDir {
			break
		}
		currentDir = filepath.Dir(currentDir)
	}

	return contextFiles
}

func prependContextFile(contextFiles []ContextFile, dir string, seenPaths map[string]bool) []ContextFile {
	contextFile, ok := loadContextFileFromDir(dir)
	if !ok {
		return contextFiles
	}
	canonicalPath := canonicalizeResourcePath(contextFile.Path)
	if seenPaths[canonicalPath] {
		return contextFiles
	}
	seenPaths[canonicalPath] = true

	return append([]ContextFile{contextFile}, contextFiles...)
}

func missingResourceDiagnostics(cwd string, paths []string, resourceType string) []ResourceDiagnostic {
	return lo.FilterMap(paths, func(rawPath string, _ int) (ResourceDiagnostic, bool) {
		resolvedPath := resolveResourcePath(rawPath, cwd)
		if resourcePathExists(resolvedPath) {
			return ResourceDiagnostic{Collision: nil, Type: "", Message: "", Path: ""}, false
		}

		return errorDiagnostic(resourceType+" path does not exist", resolvedPath), true
	})
}

func mergeResourcePaths(cwd string, primary, additional []string) []string {
	merged := make([]string, 0, len(primary)+len(additional))
	seen := map[string]bool{}
	for _, path := range append(append([]string{}, primary...), additional...) {
		resolvedPath := resolveResourcePath(path, cwd)
		canonicalPath := canonicalizeResourcePath(resolvedPath)
		if seen[canonicalPath] {
			continue
		}
		seen[canonicalPath] = true
		merged = append(merged, resolvedPath)
	}

	return merged
}

func copyResourceSnapshot(snapshot *ResourceSnapshot) ResourceSnapshot {
	return ResourceSnapshot{
		SkillDiagnostics:   append([]ResourceDiagnostic{}, snapshot.SkillDiagnostics...),
		PromptDiagnostics:  append([]ResourceDiagnostic{}, snapshot.PromptDiagnostics...),
		AppendSystemPrompt: append([]string{}, snapshot.AppendSystemPrompt...),
		ContextFiles:       append([]ContextFile{}, snapshot.ContextFiles...),
		SystemPrompt:       snapshot.SystemPrompt,
		Skills:             append([]Skill{}, snapshot.Skills...),
		Prompts:            append([]PromptTemplate{}, snapshot.Prompts...),
	}
}
