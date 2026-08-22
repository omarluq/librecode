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

	if leafEntry != nil {
		return runtime.modelContextEntityFrom(ctx, sessionID, leafEntry.ID)
	}

	return &database.SessionContextEntity{
		UsageAnchor:   nil,
		Provider:      "",
		Model:         "",
		ThinkingLevel: "",
		Messages:      []database.MessageEntity{},
	}, nil
}

func (runtime *Runtime) modelContextEntityFrom(
	ctx context.Context,
	sessionID string,
	entryID string,
) (*database.SessionContextEntity, error) {
	if strings.TrimSpace(entryID) == "" {
		return nil, oops.In("assistant").
			Code("context_entry_required").
			Errorf("context entry ID is required for session %q", sessionID)
	}

	contextEntity, err := runtime.sessions.BuildContext(ctx, sessionID, entryID)
	if err != nil {
		return nil, oops.In("assistant").Code("load_context").Wrapf(err, "load session context from entry")
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

const engineeringPrinciplesPrompt = "# Engineering principles\n\n" +
	"- Preserve documented public behavior, persisted data, and extension contracts unless the task explicitly " +
	"authorizes a breaking change. When a breaking change is authorized, remove obsolete paths instead of " +
	"adding indefinite compatibility layers, fallbacks, or migrations.\n" +
	"- Choose the simplest implementation that fully satisfies the current requirements. Avoid speculative " +
	"abstractions, configuration, and indirection.\n" +
	"- Grow the system in working end-to-end layers. Start with the smallest complete version, then add " +
	"capabilities without replacing working behavior with unfinished complexity.\n" +
	"- Keep components modular and concerns clearly separated.\n" +
	"- Prefer established, well-maintained libraries when they reduce overall complexity or improve reliability. " +
	"Do not reimplement common functionality without a clear reason.\n" +
	"- Inspect existing code, dependencies, documentation, and types before writing an implementation or adding " +
	"a package. Prefer the standard library and existing project dependencies when they meet the requirements.\n" +
	"- Make architectural decisions for the long term. Avoid knowingly temporary production implementations " +
	"intended to be replaced later unless the task explicitly requires a documented incremental step."

func (runtime *Runtime) baseSystemPrompt(cwd string) string {
	identity := "You are librecode, an AI coding assistant. Be concise, helpful, and accurate."
	toolGuidance := strings.Join([]string{
		"Use built-in tools (ls, find, grep, ast, read, fetch, bash, edit, write) " +
			"to inspect or change workspace files when needed.",
		"Use agent_start for focused independent work that can run concurrently; " +
			"it returns immediately with a task ID.",
		"Use agent_wait_all to block until every started agent finishes and collect all results at once.",
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

		if len(names) > 0 {
			toolGuidance = "Use only the available tools (" + strings.Join(names, ", ") +
				") to complete the task."
		} else {
			toolGuidance = ""
		}
	}

	sections := []string{
		strings.Join([]string{
			identity,
			"You are running inside a local filesystem workspace.",
			"Current working directory: " + cwd,
			toolGuidance,
			"Do not claim you cannot access files; inspect them with tools instead.",
			"Respect .gitignore and default ignored paths; avoid ignored files unless explicitly needed.",
			"Use the fewest tool calls needed; once you have enough evidence, stop using tools and answer.",
		}, "\n"),
		engineeringPrinciplesPrompt,
	}

	if instructions := runtime.loadAgentInstructions(cwd); instructions != "" {
		sections = append(sections, instructions)
	}

	return strings.Join(sections, "\n\n")
}
