// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func (runtime *Runtime) modelContextEntity(
	ctx context.Context,
	sessionID string,
) (*database.SessionContextEntity, error) {
	leafEntry, _, err := runtime.sessions.LeafEntry(ctx, sessionID)
	if err != nil {
		return nil, oops.In("assistant").Code("load_context_leaf").Wrapf(err, "load session leaf")
	}

	leafID := ""
	if leafEntry != nil {
		leafID = leafEntry.ID
	}

	contextEntity, err := runtime.sessions.BuildContext(ctx, sessionID, leafID)
	if err != nil {
		return nil, oops.In("assistant").Code("load_context").Wrapf(err, "load session context")
	}

	return contextEntity, nil
}

func modelFacingMessages(messages []database.MessageEntity) []database.MessageEntity {
	return model.FacingMessages(messages)
}

func remapUsageAnchor(
	anchor *database.ContextUsageAnchorEntity,
	originalMessages []database.MessageEntity,
	modelMessages []database.MessageEntity,
) *database.ContextUsageAnchorEntity {
	if anchor == nil || anchor.MessageIndex < 0 || anchor.MessageIndex >= len(originalMessages) {
		return nil
	}

	anchorMessage := originalMessages[anchor.MessageIndex]
	modelIndex := -1

	for originalIndex := range originalMessages[:anchor.MessageIndex+1] {
		message := originalMessages[originalIndex]
		if model.IsFacingMessage(&message) {
			modelIndex++
		}
	}

	if modelIndex < 0 || modelIndex >= len(modelMessages) {
		return nil
	}

	if modelMessages[modelIndex].Timestamp != anchorMessage.Timestamp {
		return nil
	}

	remapped := *anchor
	remapped.MessageIndex = modelIndex

	return &remapped
}

func (runtime *Runtime) baseSystemPrompt(cwd string) string {
	identity := "You are librecode, an AI coding assistant. Be concise, helpful, and accurate."
	toolGuidance := strings.Join([]string{
		"Use built-in tools (ls, find, grep, ast, read, fetch, bash, edit, write) " +
			"to inspect or change workspace files when needed.",
		"Use agent_start for focused independent work that can run concurrently; " +
			"it returns immediately with a task ID.",
		"Use agent_status or agent_wait to check progress without blocking, agent_list to inspect tasks, " +
			"and agent_cancel to stop work.",
		"Start independent agents before checking results so they run in parallel; " +
			"do not repeatedly poll running agents.",
	}, "\n")

	if runtime.profile.Kind != ExecutionTopLevel {
		identity = runtime.profile.SystemPrompt

		names := make([]string, 0, len(runtime.profile.Tools))
		for _, name := range runtime.profile.Tools {
			names = append(names, string(name))
		}

		toolGuidance = "Use only the available tools (" + strings.Join(names, ", ") +
			") to complete the task."
	}

	sections := []string{strings.Join([]string{
		identity,
		"You are running inside a local filesystem workspace.",
		"Current working directory: " + cwd,
		toolGuidance,
		"Do not claim you cannot access files; inspect them with tools instead.",
		"Respect .gitignore and default ignored paths; avoid ignored files unless explicitly needed.",
		"Use the fewest tool calls needed; once you have enough evidence, stop using tools and answer.",
	}, "\n")}

	if instructions := runtime.loadAgentInstructions(cwd); instructions != "" {
		sections = append(sections, instructions)
	}

	return strings.Join(sections, "\n\n")
}
