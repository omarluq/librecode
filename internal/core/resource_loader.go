package core

import (
	"context"
	"sync"
)

// ResourceSnapshot is a coherent immutable view of loaded resources.
type ResourceSnapshot struct {
	SkillDiagnostics  []ResourceDiagnostic `json:"skill_diagnostics"`
	AgentInstructions string               `json:"agent_instructions"`
	Skills            []Skill              `json:"skills"`
}

// DefaultResourceLoader loads local skills and agent instructions for a working directory.
type DefaultResourceLoader struct {
	snapshot *ResourceSnapshot
	cwd      string
	lock     sync.RWMutex
}

// NewDefaultResourceLoader creates a resource loader for cwd.
func NewDefaultResourceLoader(cwd string) *DefaultResourceLoader {
	snapshot := emptyResourceSnapshot()

	return &DefaultResourceLoader{
		snapshot: &snapshot,
		cwd:      cwd,
		lock:     sync.RWMutex{},
	}
}

// Reload refreshes the resource snapshot from disk.
func (loader *DefaultResourceLoader) Reload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return coreError(err, "reload resources")
	}

	skills := LoadSkills(loader.cwd, nil, true)
	snapshot := ResourceSnapshot{
		SkillDiagnostics:  skills.Diagnostics,
		AgentInstructions: LoadAgentInstructions(loader.cwd),
		Skills:            skills.Skills,
	}

	loader.lock.Lock()
	loader.snapshot = &snapshot
	loader.lock.Unlock()

	return coreError(ctx.Err(), "reload resources")
}

// Snapshot returns a defensive copy of the current resource state.
func (loader *DefaultResourceLoader) Snapshot() ResourceSnapshot {
	loader.lock.RLock()
	defer loader.lock.RUnlock()

	return copyResourceSnapshot(loader.snapshot)
}

func emptyResourceSnapshot() ResourceSnapshot {
	return ResourceSnapshot{
		SkillDiagnostics:  []ResourceDiagnostic{},
		AgentInstructions: "",
		Skills:            []Skill{},
	}
}

func copyResourceSnapshot(snapshot *ResourceSnapshot) ResourceSnapshot {
	return ResourceSnapshot{
		SkillDiagnostics:  append([]ResourceDiagnostic{}, snapshot.SkillDiagnostics...),
		AgentInstructions: snapshot.AgentInstructions,
		Skills:            append([]Skill{}, snapshot.Skills...),
	}
}
