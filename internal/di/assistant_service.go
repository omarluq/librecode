package di

import (
	"path/filepath"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/tool"
)

func toolCoordinator(service *ToolService) *tool.Coordinator {
	if service == nil {
		return nil
	}

	return service.Coordinator
}

// AssistantService exposes the assistant runtime.
type AssistantService struct {
	Runtime      *assistant.Runtime
	Agents       *agent.Catalog
	capabilities *runtimeCapabilities
}

type assistantDependencies struct {
	Config      *ConfigService      `do:""`
	Database    *DatabaseService    `do:""`
	Extensions  *ExtensionService   `do:""`
	Cache       *CacheService       `do:""`
	Models      *ModelService       `do:""`
	TaskRuntime *TaskRuntimeService `do:""`
	Logger      *LoggerService      `do:""`
	Tools       *ToolService        `do:""`
	Skills      *SkillsService      `do:""`
}

// NewAssistantService wires the assistant runtime.
func NewAssistantService(injector do.Injector) (*AssistantService, error) {
	dependencies, err := do.InvokeStruct[assistantDependencies](injector)
	if err != nil {
		return nil, serviceError(err, "resolve assistant dependencies")
	}

	cwd, err := filepath.Abs(".")
	if err != nil {
		return nil, serviceError(err, "resolve agent working directory")
	}

	agents := agent.Load(cwd)
	capabilities := newRuntimeCapabilities()

	var toolTasks assistant.ToolTaskController
	if dependencies.TaskRuntime != nil {
		toolTasks = dependencies.TaskRuntime.Tools
	}

	runtime := assistant.NewRuntime(&assistant.RuntimeOptions{
		Config:            dependencies.Config.Get(),
		Sessions:          dependencies.Database.Sessions,
		Extensions:        dependencies.Extensions.Manager,
		Cache:             dependencies.Cache.Responses,
		Models:            dependencies.Models.Registry,
		Client:            nil,
		Logger:            dependencies.Logger.SlogLogger,
		SkillsCache:       dependencies.Skills.Cache,
		Agents:            agents,
		AgentTasks:        capabilities,
		WorkflowSubmitter: capabilities,
		ToolTasks:         toolTasks,
		ToolCoordinator:   toolCoordinator(dependencies.Tools),
	})
	if dependencies.TaskRuntime != nil && dependencies.TaskRuntime.Tools != nil {
		dependencies.TaskRuntime.Tools.SetCompletionHook(runtime.BackgroundToolCompletion)
	}

	return &AssistantService{
		Runtime:      runtime,
		Agents:       agents,
		capabilities: capabilities,
	}, nil
}
