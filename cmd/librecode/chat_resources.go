package main

import (
	"context"

	"github.com/omarluq/librecode/internal/core"
)

func loadTerminalResources(ctx context.Context, cwd string) core.ResourceSnapshot {
	loader := core.NewDefaultResourceLoader(cwd)
	if err := loader.Reload(ctx); err != nil {
		return core.ResourceSnapshot{
			SkillDiagnostics:  []core.ResourceDiagnostic{},
			AgentInstructions: "",
			Skills:            []core.Skill{},
		}
	}

	return loader.Snapshot()
}
