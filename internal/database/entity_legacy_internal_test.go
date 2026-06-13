package database

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntryDataEntityLegacyFieldsDoNotOverrideCanonicalZeroValues(t *testing.T) {
	t.Parallel()

	content := []byte(`{
		"model_facing": false,
		"modelFacing": true,
		"from_id": "",
		"fromId": "legacy-from",
		"token_estimate": 0,
		"tokenEstimate": 42,
		"from_hook": false,
		"fromHook": true
	}`)

	var data EntryDataEntity
	require.NoError(t, json.Unmarshal(content, &data))
	require.NotNil(t, data.ModelFacing)
	assert.False(t, *data.ModelFacing)
	assert.Empty(t, data.FromID)
	assert.Zero(t, data.TokenEstimate)
	assert.False(t, data.FromHook)
}

func TestEntryTokenUsageEntityLegacyFieldsDoNotOverrideCanonicalZeroValues(t *testing.T) {
	t.Parallel()

	content := []byte(`{
		"context_window": 0,
		"contextWindow": 1000,
		"context_tokens": 0,
		"contextTokens": 300,
		"input_tokens": 0,
		"inputTokens": 250,
		"output_tokens": 0,
		"outputTokens": 50
	}`)

	var usage EntryTokenUsageEntity
	require.NoError(t, json.Unmarshal(content, &usage))
	assert.Zero(t, usage.ContextWindow)
	assert.Zero(t, usage.ContextTokens)
	assert.Zero(t, usage.InputTokens)
	assert.Zero(t, usage.OutputTokens)
}
