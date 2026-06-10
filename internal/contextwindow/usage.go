package contextwindow

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/mapsutil"
	"github.com/omarluq/librecode/internal/model"
)

const (
	// MaxContributors is the maximum number of contributors exposed in usage diagnostics.
	MaxContributors = 8
	previewRunes    = 96
)

// Breakdown returns the token breakdown for model-facing context.
func Breakdown(systemTokens, skillTokens, historyTokens int, contributions []Contribution) map[string]int {
	breakdown := map[string]int{
		BreakdownSystem:     systemTokens,
		BreakdownSkills:     skillTokens,
		BreakdownHistory:    historyTokens,
		BreakdownExtensions: 0,
	}
	for index := range contributions {
		breakdown[BreakdownExtensions] += contributions[index].Tokens
	}

	return breakdown
}

// EstimateBuildUsage estimates the current model-facing context usage.
func EstimateBuildUsage(
	systemPrompt string,
	messages []database.MessageEntity,
	contributions []Contribution,
	selectedModel *model.Model,
	breakdown map[string]int,
	usageAnchor *database.ContextUsageAnchorEntity,
) model.TokenUsage {
	inputTokens := EstimateUsageLedInputTokens(systemPrompt, messages, contributions, usageAnchor)
	contextWindow := 0
	if selectedModel != nil {
		contextWindow = selectedModel.ContextWindow
	}

	return model.TokenUsage{
		Breakdown:       mapsutil.CloneOrNil(breakdown),
		TopContributors: TopContributors(systemPrompt, messages, contributions),
		ContextWindow:   contextWindow,
		ContextTokens:   inputTokens,
		InputTokens:     inputTokens,
		OutputTokens:    0,
	}
}

// EstimateUsageLedInputTokens combines provider-reported anchor usage with local trailing estimates.
func EstimateUsageLedInputTokens(
	systemPrompt string,
	messages []database.MessageEntity,
	contributions []Contribution,
	usageAnchor *database.ContextUsageAnchorEntity,
) int {
	if usageAnchor != nil && usageAnchor.Usage.InputTokens > 0 && usageAnchor.MessageIndex >= 0 &&
		usageAnchor.MessageIndex < len(messages) {
		trailingTokens := estimateTrailingInputTokens(messages, contributions, usageAnchor.MessageIndex+1)

		return usageAnchor.Usage.InputTokens + trailingTokens
	}

	inputTokens := EstimateInputTokens(systemPrompt, messages)
	for index := range contributions {
		inputTokens += contributions[index].Tokens
	}

	return inputTokens
}

func estimateTrailingInputTokens(
	messages []database.MessageEntity,
	contributions []Contribution,
	startIndex int,
) int {
	trailingTokens := 0
	for index := startIndex; index < len(messages); index++ {
		trailingTokens += EstimateTokens(messages[index].Content)
	}
	for index := range contributions {
		trailingTokens += contributions[index].Tokens
	}

	return trailingTokens
}

// TopContributors returns the largest contributors to model-facing context.
func TopContributors(
	systemPrompt string,
	messages []database.MessageEntity,
	contributions []Contribution,
) []model.TokenContributor {
	contributors := make([]model.TokenContributor, 0, len(messages)+len(contributions)+1)
	if tokens := EstimateTokens(systemPrompt); tokens > 0 {
		contributors = append(contributors, newTokenContributor("system prompt", BreakdownSystem, systemPrompt, tokens))
	}
	for index := range messages {
		message := messages[index]
		if tokens := EstimateTokens(message.Content); tokens > 0 {
			label := fmt.Sprintf("message %d", index+1)
			contributor := newTokenContributor(label, string(message.Role), message.Content, tokens)
			contributors = append(contributors, contributor)
		}
	}
	for index := range contributions {
		contribution := contributions[index]
		label := contribution.Name
		if label == "" {
			label = fmt.Sprintf("extension contribution %d", index+1)
		}
		contributor := newTokenContributor(label, contribution.Role, contribution.Content, contribution.Tokens)
		contributors = append(contributors, contributor)
	}
	slices.SortFunc(contributors, func(left, right model.TokenContributor) int {
		return right.Tokens - left.Tokens
	})
	if len(contributors) > MaxContributors {
		contributors = contributors[:MaxContributors]
	}

	return contributors
}

func newTokenContributor(label, role, content string, tokens int) model.TokenContributor {
	return model.TokenContributor{
		Label:   label,
		Role:    role,
		Preview: ContributorPreview(content),
		Tokens:  tokens,
		Chars:   utf8.RuneCountInString(content),
	}
}

// ContributorPreview returns a compact single-line content preview.
func ContributorPreview(content string) string {
	preview := strings.Join(strings.Fields(content), " ")
	if utf8.RuneCountInString(preview) <= previewRunes {
		return preview
	}
	runes := []rune(preview)

	return string(runes[:previewRunes]) + "…"
}

// MergeUsage overlays provider-reported usage on an estimated usage snapshot.
func MergeUsage(estimated, reported model.TokenUsage) model.TokenUsage {
	usage := llm.MergeUsage(llmUsageFromModel(estimated), llmUsageFromModel(reported))

	return modelUsageFromLLM(usage)
}

// ProviderUsageEntity converts model usage into database usage metadata for persisted provider entries.
func ProviderUsageEntity(usage model.TokenUsage) *database.EntryTokenUsageEntity {
	if usage.InputTokens <= 0 && usage.ContextTokens <= 0 && usage.ContextWindow <= 0 && usage.OutputTokens <= 0 {
		return nil
	}

	return &database.EntryTokenUsageEntity{
		ContextWindow: usage.ContextWindow,
		ContextTokens: usage.ContextTokens,
		InputTokens:   usage.InputTokens,
		OutputTokens:  usage.OutputTokens,
	}
}
