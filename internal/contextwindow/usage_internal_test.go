package contextwindow

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const (
	testContributorMutationLabel = "changed-label"
	testContributorUserRole      = "user"
	testUsagePreview             = "usage preview"
)

func TestTopContributorsRankingAndFallbacks(t *testing.T) {
	t.Parallel()

	contributors := TopContributors(
		"",
		[]database.MessageEntity{
			testMessageEntity(database.RoleUser, strings.Repeat("tiny ", 4)),
			testMessageEntity(database.RoleAssistant, strings.Repeat("large assistant message ", 80)),
		},
		[]Contribution{
			testContribution("", "extension", strings.Repeat("extension context ", 120), 500),
		},
	)

	require.NotEmpty(t, contributors)
	assert.Equal(t, "extension contribution 1", contributors[0].Label)
	assert.Equal(t, "extension", contributors[0].Role)
	assert.Equal(t, 500, contributors[0].Tokens)
	assert.Contains(t, contributors[0].Preview, "extension context")

	var foundMessage bool
	for _, contributor := range contributors {
		if contributor.Label == "message 2" {
			foundMessage = true
			assert.Equal(t, string(database.RoleAssistant), contributor.Role)
			assert.Greater(t, contributor.Tokens, 0)
		}
	}
	assert.True(t, foundMessage, "expected large assistant message contributor")
}

func TestTopContributorsCapsResults(t *testing.T) {
	t.Parallel()

	messages := make([]database.MessageEntity, 0, MaxContributors+4)
	for index := range MaxContributors + 4 {
		messages = append(messages, testMessageEntity(database.RoleUser, strings.Repeat("message ", index+1)))
	}

	contributors := TopContributors("", messages, nil)

	require.Len(t, contributors, MaxContributors)
	assert.Equal(t, "message 12", contributors[0].Label)
}

func TestMergeUsageAcceptsProviderContextTokens(t *testing.T) {
	t.Parallel()

	estimated := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100_000,
		ContextTokens:   14_000,
		InputTokens:     14_000,
		OutputTokens:    0,
	}
	reported := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   12_000,
		InputTokens:     12_000,
		OutputTokens:    700,
	}

	assert.Equal(t, model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100_000,
		ContextTokens:   12_000,
		InputTokens:     12_000,
		OutputTokens:    700,
	}, MergeUsage(estimated, reported))
}

func TestMergeUsageClonesReportedBreakdownAndContributors(t *testing.T) {
	t.Parallel()

	reported := model.TokenUsage{
		Breakdown: map[string]int{
			BreakdownHistory: 10,
		},
		TopContributors: []model.TokenContributor{
			{Label: "message 1", Role: testContributorUserRole, Preview: testUsagePreview, Tokens: 10, Chars: 40},
		},
		ContextWindow: 0,
		ContextTokens: 0,
		InputTokens:   0,
		OutputTokens:  0,
	}
	merged := MergeUsage(model.EmptyTokenUsage(), reported)

	require.Equal(t, reported.Breakdown, merged.Breakdown)
	require.Equal(t, reported.TopContributors, merged.TopContributors)

	reported.Breakdown[BreakdownHistory] = 999
	reported.TopContributors[0].Label = testContributorMutationLabel

	assert.Equal(t, 10, merged.Breakdown[BreakdownHistory])
	assert.Equal(t, "message 1", merged.TopContributors[0].Label)
}

func testMessageEntity(role database.Role, content string) database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Time{},
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	}
}

func testContribution(name, role, content string, tokens int) Contribution {
	return Contribution{
		Metadata: nil,
		Source:   ContributionSourceExtension,
		Name:     name,
		Role:     role,
		Content:  content,
		Tokens:   tokens,
	}
}
