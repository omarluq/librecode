package contextwindow

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func TestEstimateUsageLedInputTokensUsesProviderAnchorAndTrailingEstimate(t *testing.T) {
	t.Parallel()

	messages := []database.MessageEntity{
		newUsageLedTestMessage(database.RoleUser, repeatedTokenText(100)),
		newUsageLedTestMessage(database.RoleAssistant, repeatedTokenText(100)),
		newUsageLedTestMessage(database.RoleUser, repeatedTokenText(40)),
	}
	tests := []struct {
		name       string
		usage      database.EntryTokenUsageEntity
		wantTokens int
	}{
		{
			name: "prefers latest request context over cumulative billed input",
			usage: database.EntryTokenUsageEntity{
				ContextWindow: 0,
				ContextTokens: 300,
				InputTokens:   1_200,
				OutputTokens:  0,
			},
			wantTokens: 340,
		},
		{
			name: "locally estimates legacy anchors without context tokens",
			usage: database.EntryTokenUsageEntity{
				ContextWindow: 0,
				ContextTokens: 0,
				InputTokens:   500,
				OutputTokens:  0,
			},
			wantTokens: 255,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			anchor := &database.ContextUsageAnchorEntity{
				EntryID:      "assistant-entry",
				Provider:     "",
				Model:        "",
				Usage:        test.usage,
				MessageIndex: 1,
			}

			tokens := EstimateUsageLedInputTokens(
				"large system prompt that should be covered by provider usage",
				messages,
				nil,
				anchor,
			)

			assert.Equal(t, test.wantTokens, tokens)
		})
	}
}

func TestEstimateUsageLedInputTokensFallsBackWhenAnchorMissing(t *testing.T) {
	t.Parallel()

	messages := []database.MessageEntity{newUsageLedTestMessage(database.RoleUser, repeatedTokenText(20))}
	contributions := []Contribution{{
		Metadata: nil,
		Source:   "",
		Name:     "",
		Role:     "",
		Content:  "",
		Tokens:   7,
	}}

	tokens := EstimateUsageLedInputTokens(repeatedTokenText(12), messages, contributions, nil)

	assert.Equal(t, 39, tokens)
}

func TestProviderUsageEntitySkipsEmptyUsage(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ProviderUsageEntity(model.EmptyTokenUsage()))

	entity := ProviderUsageEntity(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1000,
		ContextTokens:   50,
		InputTokens:     42,
		OutputTokens:    8,
	})

	require.NotNil(t, entity)
	assert.Equal(t, 1000, entity.ContextWindow)
	assert.Equal(t, 50, entity.ContextTokens)
	assert.Equal(t, 42, entity.InputTokens)
	assert.Equal(t, 8, entity.OutputTokens)
}

func newUsageLedTestMessage(role database.Role, content string) database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Time{},
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	}
}

func repeatedTokenText(tokens int) string {
	text := make([]byte, tokens*4)
	for index := range text {
		text[index] = 'x'
	}

	return string(text)
}
