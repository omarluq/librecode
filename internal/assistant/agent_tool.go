package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	agentToolName            tool.Name = "agent"
	maxAgentPromptBytes                = 32 * 1024
	maxChildSessionNameRunes           = 80
)

func schemaForAgentTool() tool.Schema {
	const rawSchema = `{
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"agent":{"type":"string","description":"Name of the subagent to invoke."},
			"prompt":{"type":"string","description":"Focused task for the subagent."}
		},
		"required":["agent","prompt"]
	}`

	schema, err := tool.SchemaFromRaw([]byte(rawSchema))
	if err != nil {
		panic(err)
	}

	return schema
}

type agentToolInput struct {
	Agent  string `json:"agent"`
	Prompt string `json:"prompt"`
}

type agentToolExecutor struct {
	runtime         *Runtime
	catalog         *agent.Catalog
	parentSessionID string
	cwd             string
}

func (executor *agentToolExecutor) Definition() tool.Definition {
	available := executor.catalog.Definitions()

	descriptions := make([]string, 0, len(available))
	for index := range available {
		definition := &available[index]
		descriptions = append(descriptions, definition.Name+": "+definition.Description)
	}

	return tool.Definition{
		Schema: schemaForAgentTool(),
		Name:   agentToolName,
		Label:  string(agentToolName),
		Description: "Delegate a focused task to an isolated read-only subagent. Available agents: " +
			strings.Join(descriptions, "; "),
		PromptSnippet: "Delegate focused read-only exploration to a subagent",
		PromptGuidelines: []string{
			"Use an agent for focused repository exploration that can proceed independently.",
			"Give the agent a specific task and expected output.",
		},
		ReadOnly: true,
	}
}

func (executor *agentToolExecutor) Execute(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	var args agentToolInput
	if err := input.Decode(&args); err != nil {
		return tool.TextResult("", nil), err
	}

	args.Agent = strings.ToLower(strings.TrimSpace(args.Agent))

	args.Prompt = strings.TrimSpace(args.Prompt)
	if args.Agent == "" || args.Prompt == "" {
		return tool.TextResult("", nil), errors.New("agent and prompt are required")
	}

	if len(args.Prompt) > maxAgentPromptBytes || !utf8.ValidString(args.Prompt) {
		return tool.TextResult("", nil), fmt.Errorf(
			"agent prompt must be valid UTF-8 and at most %d bytes",
			maxAgentPromptBytes,
		)
	}

	definition, found := executor.catalog.Get(args.Agent)
	if !found {
		names := make([]string, 0, len(executor.catalog.Definitions()))

		available := executor.catalog.Definitions()
		for index := range available {
			names = append(names, available[index].Name)
		}

		return tool.TextResult("", nil), fmt.Errorf(
			"unknown agent %q; available agents: %s",
			args.Agent,
			strings.Join(names, ", "),
		)
	}

	childSession, err := executor.runtime.sessions.CreateSession(
		ctx,
		executor.cwd,
		childSessionName(definition.Name, args.Prompt),
		executor.parentSessionID,
	)
	if err != nil {
		return tool.TextResult("", nil), oops.In("assistant").Code("create_agent_session").Wrapf(
			err,
			"create child agent session",
		)
	}

	child := executor.runtime.childRuntime(&definition)

	response, err := child.Prompt(ctx, &PromptRequest{
		OnEvent: nil, OnRetry: nil, OnUserEntry: nil, ParentEntryID: nil,
		SessionID: childSession.ID, CWD: executor.cwd, Text: args.Prompt, Name: "", ResumeLatest: false,
	})
	if err != nil {
		return tool.TextResult("", map[string]any{"agent": definition.Name, "session_id": childSession.ID}), err
	}

	return tool.TextResult(response.Text, map[string]any{
		"agent": definition.Name, "session_id": childSession.ID, "usage": response.Usage,
	}), nil
}

func childSessionName(agentName, prompt string) string {
	name := agentName + ": " + strings.Join(strings.Fields(prompt), " ")

	runes := []rune(name)
	if len(runes) > maxChildSessionNameRunes {
		name = string(runes[:maxChildSessionNameRunes-1]) + "…"
	}

	return name
}
