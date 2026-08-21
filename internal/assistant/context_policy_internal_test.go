package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/model"
)

func TestCompactionModelUsesDedicatedCatalogEntry(t *testing.T) {
	t.Parallel()

	var conversation model.Model

	conversation.Provider, conversation.ID = "chat", delegationLargeModel
	conversation.ContextWindow, conversation.MaxTokens = 100_000, 8_000

	var dedicated model.Model

	dedicated.Provider, dedicated.ID = "summary", delegationSmallModel
	dedicated.ContextWindow, dedicated.MaxTokens = 32_000, 2_000

	var cfg config.Config

	cfg.Assistant.Provider, cfg.Assistant.Model = conversation.Provider, conversation.ID
	cfg.Context.CompactionProvider, cfg.Context.CompactionModel = dedicated.Provider, dedicated.ID

	var registryOptions model.RegistryOptions

	registryOptions.BuiltIns = []model.Model{conversation, dedicated}

	var runtime Runtime

	runtime.cfg = &cfg
	runtime.models = model.NewRegistry(&registryOptions)

	got, err := runtime.compactionModel()
	require.NoError(t, err)
	assert.Equal(t, dedicated.Provider, got.Provider)
	assert.Equal(t, dedicated.ID, got.ID)

	runtime.cfg.Context.CompactionModel = runtimeSlashMissing
	_, err = runtime.compactionModel()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configured compaction model summary/missing is unavailable")
}

func TestContextPolicyControlsThresholdTailAndSummaryOutput(t *testing.T) {
	t.Parallel()

	var cfg config.Config

	cfg.Context.AutoCompactionThreshold = 90
	cfg.Context.RetainedTailMaxTokens = 1_000
	cfg.Context.SummaryOutputTokens = 500

	var runtime Runtime

	runtime.cfg = &cfg

	var budget contextwindow.Budget

	budget.ContextWindow, budget.UsableInput, budget.InputTokens = 10_000, 1_000, 899
	assert.False(t, runtime.shouldAutoCompactAfterResponse(budget))
	budget.InputTokens = 900
	assert.True(t, runtime.shouldAutoCompactAfterResponse(budget))

	var tailModel model.Model

	tailModel.ContextWindow = 90_000
	assert.Equal(t, 1_000, runtime.compactionRecentTailTokens(&tailModel, 60_000))

	var summaryModel model.Model

	summaryModel.ContextWindow, summaryModel.MaxTokens = 10_000, 4_000
	limit, err := runtime.summaryOutputLimit(&summaryModel)
	require.NoError(t, err)
	assert.Equal(t, 500, limit)
}
