package compaction

import (
	"errors"
	"fmt"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
)

// SemanticGroupKind identifies an indivisible summary input unit.
type SemanticGroupKind string

const (
	// SemanticGroupHistoryTurn identifies an original conversation turn.
	SemanticGroupHistoryTurn SemanticGroupKind = "history_turn"
	// SemanticGroupReduction identifies a prior reduction result.
	SemanticGroupReduction SemanticGroupKind = "reduction"
)

// SemanticGroup keeps a durable turn, including all tool results, intact.
type SemanticGroup struct {
	Kind     SemanticGroupKind
	EntryIDs []string
	Messages []database.MessageEntity
	Tokens   int
}

// GroupsFromMessages groups model-facing history at user/custom/bash turn boundaries.
// Tokens use the same complete-message estimator as summary request preflight.
func GroupsFromMessages(messages []database.MessageEntity, entryIDs []string, _ TokenCounter) []SemanticGroup {
	groups := make([]SemanticGroup, 0)

	for index := range messages {
		message := messages[index]
		startsTurn := startsSemanticTurn(message.Role)

		if len(groups) == 0 || startsTurn {
			groups = append(groups, SemanticGroup{
				Kind:     SemanticGroupHistoryTurn,
				EntryIDs: nil,
				Messages: nil,
				Tokens:   0,
			})
		}

		group := &groups[len(groups)-1]
		group.Messages = append(group.Messages, message)

		if index < len(entryIDs) && entryIDs[index] != "" {
			group.EntryIDs = append(group.EntryIDs, entryIDs[index])
		}

		group.Tokens += contextwindow.EstimateMessageTokens([]database.MessageEntity{message})
	}

	return groups
}

const (
	// MaxChunksPerReductionRound bounds model calls in one reduction round.
	MaxChunksPerReductionRound = 32
	// MaxReductionDepth bounds recursive summary reduction rounds.
	MaxReductionDepth = 4
)

// Chunk is a deterministic, protocol-safe summary request unit.
type Chunk struct {
	Groups   []SemanticGroup
	Messages []database.MessageEntity
	Tokens   int
}

// Partition greedily packs the largest stable prefix of semantic groups.
func Partition(groups []SemanticGroup, availableInputTokens, maxChunks int) ([]Chunk, error) {
	if availableInputTokens <= 0 {
		return nil, fmt.Errorf("%w: available=%d", ErrSummaryFixedOverhead, availableInputTokens)
	}

	if maxChunks <= 0 || maxChunks > MaxChunksPerReductionRound {
		maxChunks = MaxChunksPerReductionRound
	}

	chunks := make([]Chunk, 0)

	for index := 0; index < len(groups); {
		if err := validateSemanticGroup(groups[index], index, availableInputTokens); err != nil {
			return nil, err
		}

		chunk, nextIndex := packChunk(groups, index, availableInputTokens)
		if len(chunk.Groups) == 0 {
			return nil, fmt.Errorf("%w", ErrSummaryIndivisibleGroup)
		}

		chunks = append(chunks, chunk)
		if len(chunks) > maxChunks {
			return nil, fmt.Errorf("summary reduction exceeds maximum chunks %d", maxChunks)
		}

		index = nextIndex
	}

	if len(chunks) == 0 {
		return nil, errors.New("summary reduction produced no chunks")
	}

	return chunks, nil
}

func validateSemanticGroup(group SemanticGroup, index, availableInputTokens int) error {
	if group.Tokens <= 0 || len(group.Messages) == 0 {
		return fmt.Errorf("invalid empty semantic group at index %d", index)
	}

	if group.Kind == SemanticGroupHistoryTurn && !startsSemanticTurn(group.Messages[0].Role) {
		return fmt.Errorf("orphaned protocol message at semantic group %d", index)
	}

	if group.Tokens > availableInputTokens {
		return fmt.Errorf(
			"%w: group_tokens=%d available=%d",
			ErrSummaryIndivisibleGroup,
			group.Tokens,
			availableInputTokens,
		)
	}

	return nil
}

func packChunk(
	groups []SemanticGroup,
	start, availableInputTokens int,
) (chunk Chunk, nextIndex int) {
	chunk = Chunk{Groups: nil, Messages: nil, Tokens: 0}
	index := start

	for index < len(groups) && chunk.Tokens+groups[index].Tokens <= availableInputTokens {
		group := groups[index]
		if group.Tokens > 0 && len(group.Messages) > 0 {
			chunk.Groups = append(chunk.Groups, group)
			chunk.Messages = append(chunk.Messages, group.Messages...)
			chunk.Tokens += group.Tokens
		}

		index++
	}

	return chunk, index
}

func startsSemanticTurn(role database.Role) bool {
	return role == database.RoleUser || role == database.RoleCustom || role == database.RoleBashExecution
}
