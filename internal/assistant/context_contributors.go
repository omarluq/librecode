package assistant

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const (
	maxContextContributors = 8
	contextPreviewRunes    = 96
)

func topContextContributors(
	systemPrompt string,
	messages []database.MessageEntity,
	contributions []contextContribution,
) []model.TokenContributor {
	contributors := make([]model.TokenContributor, 0, len(messages)+len(contributions)+1)
	if tokens := estimateTokens(systemPrompt); tokens > 0 {
		contributors = append(contributors, newTokenContributor("system prompt", "system", systemPrompt, tokens))
	}
	for index := range messages {
		message := messages[index]
		if tokens := estimateTokens(message.Content); tokens > 0 {
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
	if len(contributors) > maxContextContributors {
		contributors = contributors[:maxContextContributors]
	}

	return contributors
}

func newTokenContributor(label, role, content string, tokens int) model.TokenContributor {
	return model.TokenContributor{
		Label:   label,
		Role:    role,
		Preview: contextContributorPreview(content),
		Tokens:  tokens,
		Chars:   utf8.RuneCountInString(content),
	}
}

func contextContributorPreview(content string) string {
	preview := strings.Join(strings.Fields(content), " ")
	if utf8.RuneCountInString(preview) <= contextPreviewRunes {
		return preview
	}
	runes := []rune(preview)

	return string(runes[:contextPreviewRunes]) + "…"
}
