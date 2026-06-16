package core

import (
	"context"
	"sync"

	"github.com/samber/lo"
)

// ResourcePath is an extension-provided resource path plus optional source metadata.
type ResourcePath struct {
	SourceInfo *SourceInfo `json:"source_info,omitempty"`
	Path       string      `json:"path"`
}

// ResourceExtensionPaths contains runtime resource paths contributed by extensions.
type ResourceExtensionPaths struct {
	SkillPaths []ResourcePath `json:"skill_paths"`
}

// ResourceLoaderOptions configures DefaultResourceLoader.
type ResourceLoaderOptions struct {
	CWD                  string   `json:"cwd"`
	AdditionalSkillPaths []string `json:"additional_skill_paths"`
	NoSkills             bool     `json:"no_skills"`
}

// ResourceSnapshot is a coherent immutable view of loaded resources.
type ResourceSnapshot struct {
	SkillDiagnostics  []ResourceDiagnostic `json:"skill_diagnostics"`
	AgentInstructions string               `json:"agent_instructions"`
	Skills            []Skill              `json:"skills"`
}

// ResourceLoader exposes reloadable skill resources.
type ResourceLoader interface {
	Reload(ctx context.Context) error
	ExtendResources(ctx context.Context, paths ResourceExtensionPaths) error
	Snapshot() ResourceSnapshot
	Skills() LoadSkillsResult
}

// DefaultResourceLoader loads local skills from user, project, and explicit paths.
type DefaultResourceLoader struct {
	skillSourceInfos     map[string]SourceInfo
	cwd                  string
	snapshot             ResourceSnapshot
	additionalSkillPaths []string
	lock                 sync.RWMutex
	noSkills             bool
}

// NewDefaultResourceLoader creates a resource loader with librecode-compatible defaults.
func NewDefaultResourceLoader(options *ResourceLoaderOptions) *DefaultResourceLoader {
	resolvedOptions := resourceLoaderOptions(options)

	return &DefaultResourceLoader{
		skillSourceInfos:     map[string]SourceInfo{},
		snapshot:             emptyResourceSnapshot(),
		additionalSkillPaths: append([]string{}, resolvedOptions.AdditionalSkillPaths...),
		cwd:                  resolvedOptions.CWD,
		lock:                 sync.RWMutex{},
		noSkills:             resolvedOptions.NoSkills,
	}
}

// Reload refreshes all resource snapshots from disk.
func (loader *DefaultResourceLoader) Reload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return coreError(err, "reload resources")
	}

	snapshot := emptyResourceSnapshot()
	snapshot.AgentInstructions = LoadAgentInstructions(loader.cwd)
	snapshot.Skills, snapshot.SkillDiagnostics = loader.loadSkillsSnapshot()

	loader.lock.Lock()
	loader.snapshot = snapshot
	loader.lock.Unlock()

	return coreError(ctx.Err(), "reload resources")
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

	return LoadSkillsResult{
		Skills:            snapshot.Skills,
		AgentInstructions: snapshot.AgentInstructions,
		Diagnostics:       snapshot.SkillDiagnostics,
	}
}

func resourceLoaderOptions(options *ResourceLoaderOptions) *ResourceLoaderOptions {
	if options != nil {
		return options
	}

	return &ResourceLoaderOptions{
		AdditionalSkillPaths: nil,
		CWD:                  "",
		NoSkills:             false,
	}
}

func emptyResourceSnapshot() ResourceSnapshot {
	return ResourceSnapshot{
		SkillDiagnostics:  []ResourceDiagnostic{},
		AgentInstructions: "",
		Skills:            []Skill{},
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
}

func (loader *DefaultResourceLoader) applySkillSourceInfo(skills []Skill) []Skill {
	return lo.Map(skills, func(skill Skill, _ int) Skill {
		if sourceInfo, ok := loader.findSourceInfo(skill.FilePath, loader.skillSourceInfos); ok {
			skill.SourceInfo = sourceInfo
		}

		return skill
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
		SkillDiagnostics:  append([]ResourceDiagnostic{}, snapshot.SkillDiagnostics...),
		AgentInstructions: snapshot.AgentInstructions,
		Skills:            append([]Skill{}, snapshot.Skills...),
	}
}
