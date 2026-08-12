package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/llmconv"
)

// persistCompletedRoundWithPersistence appends one settled provider round in
// model-observed order. It is intentionally separate from steering admission:
// callers must make the round durable before exposing later user messages to a
// provider continuation.
func (runtime *Runtime) persistCompletedRoundWithPersistence(
	ctx context.Context,
	sessionID string,
	lineage *promptLineage,
	round *llm.CompletedRound,
) (*database.EntryEntity, error) {
	if lineage == nil {
		return nil, oops.In("assistant").Code("round_checkpoint_lineage").
			Errorf("persist completed round: prompt lineage is required")
	}

	if round == nil {
		return nil, oops.In("assistant").Code("nil_completed_round").
			Errorf("persist completed round: completed round is required")
	}

	bundle := responseBundleFromCompletedRound(round)

	completedNestedTools := []ToolEvent(nil)
	if lineage.progress != nil {
		completedNestedTools = append(completedNestedTools, lineage.progress.completedNestedTools...)
		bundle.ToolEvents = mergeNestedToolEvents(bundle.ToolEvents, completedNestedTools)
	}

	persistCtx, cancel := runtime.promptPersistenceContext(ctx, assistantBundleWriteCount(bundle))
	defer cancel()

	entry, err := runtime.persistAssistantBundle(persistCtx, sessionID, lineage, bundle)
	if err != nil {
		return nil, oops.In("assistant").Code("persist_completed_round").
			Wrapf(err, "persist completed provider round")
	}

	lineage.adopt(entry)
	lineage.committedThinking += len(thinkingFromLLMParts(round.Assistant.Content))
	lineage.committedTools += len(round.ToolResults)

	lineage.latestRoundUsage = llmconv.UsageToModel(round.Usage)
	if lineage.progress != nil {
		lineage.progress.commitCompletedRound(round, completedNestedTools)
	}

	return entry, nil
}

func responseBundleFromCompletedRound(round *llm.CompletedRound) *responseBundle {
	if round == nil {
		return nil
	}

	usage := llmconv.UsageToModel(round.Usage)

	return &responseBundle{
		Text:          textFromLLMParts(round.Assistant.Content),
		Thinking:      thinkingFromLLMParts(round.Assistant.Content),
		ToolEvents:    toolEventsFromLLMResults(round.ToolResults),
		Usage:         usage,
		ProviderUsage: usage,
		ModelFacing:   true,
	}
}

func (runtime *Runtime) roundCheckpoint(sessionID string, lineage *promptLineage) llm.RoundCheckpoint {
	if runtime == nil || runtime.steering == nil || lineage == nil {
		return nil
	}

	return func(ctx context.Context, round *llm.CompletedRound) ([]llm.Message, error) {
		if round == nil {
			return nil, oops.In("assistant").Code("nil_completed_round").
				Errorf("checkpoint completed provider round: completed round is required")
		}

		lineage.pendingRounds = append(lineage.pendingRounds, *round)

		drafts, err := runtime.drainSteering(sessionID, lineage, round.FinishReason)
		if err != nil {
			return nil, oops.In("assistant").Code("steering_drain").Wrapf(err, "drain steering inbox")
		}

		if len(drafts) == 0 {
			return nil, nil
		}

		if err := runtime.persistPendingRounds(ctx, sessionID, lineage); err != nil {
			return nil, runtime.restoreSteeringAfterCheckpointFailure(sessionID, lineage.runID, drafts, err)
		}

		lineage.checkpointed = true

		return runtime.persistSteeringDrafts(ctx, sessionID, lineage, drafts)
	}
}

func (runtime *Runtime) drainSteering(
	sessionID string,
	lineage *promptLineage,
	finishReason llm.FinishReason,
) ([]steeringDraft, error) {
	if finishReason == llm.FinishReasonToolCalls {
		return runtime.steering.drain(sessionID, lineageRunID(lineage))
	}

	return runtime.steering.drainFinal(sessionID, lineageRunID(lineage))
}

func (runtime *Runtime) persistPendingRounds(ctx context.Context, sessionID string, lineage *promptLineage) error {
	for len(lineage.pendingRounds) > 0 {
		pending := &lineage.pendingRounds[0]
		if _, err := runtime.persistCompletedRoundWithPersistence(ctx, sessionID, lineage, pending); err != nil {
			return err
		}

		lineage.pendingRounds = lineage.pendingRounds[1:]
	}

	return nil
}

func (runtime *Runtime) persistSteeringDrafts(
	ctx context.Context,
	sessionID string,
	lineage *promptLineage,
	drafts []steeringDraft,
) ([]llm.Message, error) {
	messages := make([]llm.Message, 0, len(drafts))
	for index := range drafts {
		entry, err := runtime.persistSteeringDraft(ctx, sessionID, lineage, drafts[index])
		if err != nil {
			return nil, runtime.restoreSteeringAfterCheckpointFailure(sessionID, lineage.runID, drafts[index:], err)
		}

		runtime.dispatchMessageAppend(ctx, entry)

		if lineage.onEvent != nil {
			encoded, marshalErr := json.Marshal(SteeringConsumedEvent{
				EntryID: entry.ID, Text: drafts[index].Text, Images: cloneSteeringDraft(drafts[index]).Images,
				HideUserPrompt: drafts[index].HideUserPrompt,
			})
			if marshalErr != nil {
				encodedErr := oops.In("assistant").Code("encode_consumed_steering").
					Wrapf(marshalErr, "encode consumed steering event")

				return nil, runtime.restoreSteeringAfterCheckpointFailure(
					sessionID, lineage.runID, drafts[index+1:], encodedErr,
				)
			}

			lineage.onEvent(StreamEvent{
				ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
				Kind: StreamEventSteeringConsumed, Text: string(encoded),
			})
		}

		messages = append(messages, llmMessageFromSteeringDraft(drafts[index]))
	}

	return messages, nil
}

func (runtime *Runtime) restoreSteeringAfterCheckpointFailure(
	sessionID string,
	runID string,
	drafts []steeringDraft,
	checkpointErr error,
) error {
	if restoreErr := runtime.steering.restore(sessionID, runID, drafts); restoreErr != nil {
		return errors.Join(checkpointErr, oops.In("assistant").Code("steering_restore").
			Wrapf(restoreErr, "restore steering after checkpoint failure"))
	}

	return checkpointErr
}

func lineageRunID(lineage *promptLineage) string {
	if lineage == nil {
		return ""
	}

	return lineage.runID
}

func (runtime *Runtime) persistSteeringDraft(
	ctx context.Context,
	sessionID string,
	lineage *promptLineage,
	draft steeringDraft,
) (*database.EntryEntity, error) {
	if lineage == nil {
		return nil, oops.In("assistant").Code("steering_lineage").
			Errorf("persist steering message: lineage is required")
	}

	persistCtx, cancel := runtime.promptPersistenceContext(ctx, 1)
	defer cancel()

	parentID := lineage.activeParentEntryID

	entry, err := runtime.appendUserPromptEntry(
		persistCtx,
		sessionID,
		&parentID,
		draft.Text,
		draft.Images,
		!draft.HideUserPrompt,
	)
	if err != nil {
		return nil, oops.In("assistant").Code("persist_steering_message").Wrapf(err, "persist steering message")
	}

	lineage.adopt(entry)

	return entry, nil
}

func llmMessageFromSteeringDraft(draft steeringDraft) llm.Message {
	message, _ := llmMessageFromDatabase(&database.MessageEntity{
		Timestamp: time.Time{},
		Role:      database.RoleUser,
		Content:   draft.Text,
		Provider:  "",
		Model:     "",
		Parts:     databasePartsFromSteeringDraft(draft),
	})

	return message
}

func databasePartsFromSteeringDraft(draft steeringDraft) []database.MessagePartEntity {
	parts := make([]database.MessagePartEntity, 0, len(draft.Images)+1)
	if draft.Text != "" {
		parts = append(parts, database.MessagePartEntity{
			Text: draft.Text, MIMEType: "", Name: "", Type: database.MessagePartText,
			Data: nil, Width: 0, Height: 0,
		})
	}

	for index := range draft.Images {
		image := &draft.Images[index]
		parts = append(parts, database.MessagePartEntity{
			Text: "", MIMEType: image.MIMEType, Name: image.Name, Type: database.MessagePartImage,
			Data: image.Data, Width: image.Width, Height: image.Height,
		})
	}

	return parts
}

func toolEventsFromLLMResults(results []llm.ToolResult) []ToolEvent {
	if len(results) == 0 {
		return nil
	}

	events := make([]ToolEvent, len(results))
	for index := range results {
		events[index] = toolEventFromLLMToolResult(&results[index])
	}

	return events
}
