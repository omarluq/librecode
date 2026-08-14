package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextPolicyDefaults(t *testing.T) {
	t.Parallel()

	cfg := Load("").MustGet()

	assert.True(t, cfg.Context.AutoCompactionEnabled)
	assert.Equal(t, 80, cfg.Context.AutoCompactionThreshold)
	assert.Equal(t, 64_000, cfg.Context.RetainedTailMaxTokens)
	assert.Zero(t, cfg.Context.SummaryOutputTokens)
	assert.Equal(t, 8_192, cfg.Context.ExtensionContributionTokens)
	assert.Empty(t, cfg.Context.CompactionProvider)
	assert.Empty(t, cfg.Context.CompactionModel)
}

func TestContextPolicyValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*ContextConfig)
		want   string
	}{
		{"zero threshold", setAutoCompactionThreshold(0), "context.auto_compaction_threshold"},
		{"high threshold", setAutoCompactionThreshold(101), "context.auto_compaction_threshold"},
		{"zero retained tail", setRetainedTailMaxTokens(0), "context.retained_tail_max_tokens"},
		{"small summary", setSummaryOutputTokens(63), "context.summary_output_tokens"},
		{"provider only", setCompactionProvider("openai"), "must be configured together"},
		{"model only", setCompactionModel("small"), "must be configured together"},
		{
			"zero extension aggregate",
			setExtensionContributionTokens(0),
			"context.extension_contribution_tokens",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg := Load("").MustGet()
			test.mutate(&cfg.Context)
			err := cfg.validateContext()
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func setAutoCompactionThreshold(value int) func(*ContextConfig) {
	return func(contextConfig *ContextConfig) { contextConfig.AutoCompactionThreshold = value }
}

func setRetainedTailMaxTokens(value int) func(*ContextConfig) {
	return func(contextConfig *ContextConfig) { contextConfig.RetainedTailMaxTokens = value }
}

func setSummaryOutputTokens(value int) func(*ContextConfig) {
	return func(contextConfig *ContextConfig) { contextConfig.SummaryOutputTokens = value }
}

func setCompactionProvider(value string) func(*ContextConfig) {
	return func(contextConfig *ContextConfig) { contextConfig.CompactionProvider = value }
}

func setCompactionModel(value string) func(*ContextConfig) {
	return func(contextConfig *ContextConfig) { contextConfig.CompactionModel = value }
}

func setExtensionContributionTokens(value int) func(*ContextConfig) {
	return func(contextConfig *ContextConfig) { contextConfig.ExtensionContributionTokens = value }
}
