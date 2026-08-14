package model

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageTotalsFromTokenUsage(t *testing.T) {
	t.Parallel()

	reportedUsage := TokenUsage{Provenance: UsageProvenance(""),
		Breakdown: nil, TopContributors: nil, ContextWindow: 0, ContextTokens: 0,
		InputTokens: 2, OutputTokens: 3,
	}
	reported, err := UsageTotalsFromTokenUsage(&reportedUsage)
	require.NoError(t, err)
	assert.Equal(t, UsageTotals{
		InputTokens: 2, OutputTokens: 3, ProviderRoundTrips: 0, Reported: true,
	}, reported)

	emptyUsage := EmptyTokenUsage()
	unknown, err := UsageTotalsFromTokenUsage(&emptyUsage)
	require.NoError(t, err)
	assert.False(t, unknown.Reported)

	reportedZeroUsage := emptyUsage.WithReported()
	reportedZero, err := UsageTotalsFromTokenUsage(&reportedZeroUsage)
	require.NoError(t, err)
	assert.Equal(t, UsageTotals{InputTokens: 0, OutputTokens: 0, ProviderRoundTrips: 0, Reported: true}, reportedZero)

	negativeUsage := TokenUsage{Provenance: UsageProvenance(""),
		Breakdown: nil, TopContributors: nil, ContextWindow: 0, ContextTokens: 0,
		InputTokens: -1, OutputTokens: 0,
	}
	_, err = UsageTotalsFromTokenUsage(&negativeUsage)
	assert.ErrorContains(t, err, "negative")
}

func TestUsageTotalsJSONCompatibilityAndValidation(t *testing.T) {
	t.Parallel()

	var unknown UsageTotals
	require.NoError(t, json.Unmarshal([]byte(`{}`), &unknown))
	assert.False(t, unknown.Reported)

	var legacy UsageTotals
	require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":2,"output_tokens":3,"extra":true}`), &legacy))
	assert.Equal(t, UsageTotals{InputTokens: 2, OutputTokens: 3, ProviderRoundTrips: 0, Reported: true}, legacy)

	var zero UsageTotals
	require.NoError(t, json.Unmarshal([]byte(`{"reported":true}`), &zero))
	assert.True(t, zero.Reported)
	encoded, err := json.Marshal(zero)
	require.NoError(t, err)
	assert.JSONEq(t, `{"reported":true}`, string(encoded))

	require.Error(t, json.Unmarshal([]byte(`{"input_tokens":-1}`), new(UsageTotals)))

	_, err = (UsageTotals{
		InputTokens: math.MaxInt64, OutputTokens: 1, ProviderRoundTrips: 0, Reported: false,
	}).TotalTokens()
	assert.ErrorContains(t, err, "overflows")
}

func TestAggregateUsage(t *testing.T) {
	t.Parallel()

	t.Run("tracks partial knowledge", func(t *testing.T) {
		t.Parallel()

		aggregate, err := AggregateUsage([]UsageTotals{
			{InputTokens: 2, OutputTokens: 1, ProviderRoundTrips: 0, Reported: true},
			{InputTokens: 0, OutputTokens: 0, ProviderRoundTrips: 0, Reported: false},
			{InputTokens: 0, OutputTokens: 0, ProviderRoundTrips: 0, Reported: true},
		})
		require.NoError(t, err)
		assert.Equal(t, 2, aggregate.Known)
		assert.Equal(t, 3, aggregate.Total)
		assert.Equal(t, int64(3), aggregate.Usage.InputTokens+aggregate.Usage.OutputTokens)
		assert.True(t, aggregate.Usage.Reported)
	})

	t.Run("rejects aggregate overflow", func(t *testing.T) {
		t.Parallel()

		_, err := AggregateUsage([]UsageTotals{
			{InputTokens: math.MaxInt64, OutputTokens: 0, ProviderRoundTrips: 0, Reported: true},
			{InputTokens: 1, OutputTokens: 0, ProviderRoundTrips: 0, Reported: true},
		})
		assert.ErrorContains(t, err, "overflows")
	})
}
