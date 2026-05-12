package main

import (
	"context"
	"path/filepath"

	"github.com/omarluq/librecode/internal/core"
)

func loadTerminalResources(ctx context.Context, cwd string) core.ResourceSnapshot {
	loader := core.NewDefaultResourceLoader(&core.ResourceLoaderOptions{
		AdditionalSkillPaths:          nil,
		AdditionalPromptTemplatePaths: nil,
		AppendSystemPrompt:            nil,
		SystemPrompt:                  "",
		AgentDir:                      terminalAgentDir(),
		CWD:                           cwd,
		NoPromptTemplates:             false,
		NoContextFiles:                false,
		NoSkills:                      false,
	})
	if err := loader.Reload(ctx); err != nil {
		return core.ResourceSnapshot{
			SkillDiagnostics:   nil,
			PromptDiagnostics:  nil,
			AppendSystemPrompt: nil,
			ContextFiles:       nil,
			SystemPrompt:       "",
			Skills:             nil,
			Prompts:            nil,
		}
	}

	return loader.Snapshot()
}

func terminalAgentDir() string {
	home, err := core.LibrecodeHome()
	if err != nil {
		return filepath.Join(".", core.ConfigDirName)
	}

	return home
}
